// Package skill provides the application service orchestrating SysSkill use cases.
package skill

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	assembler "github.com/good-fish-man/agent-runtime-client/application/assembler/skill"
	dto "github.com/good-fish-man/agent-runtime-client/application/dto/skill"
	srv "github.com/good-fish-man/agent-runtime-client/domain/srv/skill"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
)

// SysSkillService is the application service for skill configuration.
type SysSkillService struct {
	asm        *assembler.SysSkillAssembler
	srv        *srv.SysSkillSvc
	skillsRoot string
}

// NewSysSkillService wires the service over the shared data handle.
func NewSysSkillService(d *data.Data) *SysSkillService {
	return &SysSkillService{
		asm:        assembler.NewSysSkillAssembler(),
		srv:        srv.NewSysSkillSvc(d),
		skillsRoot: defaultSkillsRoot(),
	}
}

func (s *SysSkillService) CreateSysSkill(ctx context.Context, req *dto.CreateSysSkillReq) (*dto.CreateSysSkillRsp, error) {
	en := s.asm.D2ECreate(req)
	ulid, err := s.srv.Create(ctx, en)
	if err != nil {
		return nil, err
	}
	return &dto.CreateSysSkillRsp{Ulid: ulid}, nil
}

func (s *SysSkillService) DeleteSysSkill(ctx context.Context, req *dto.DelSysSkillReq) error {
	existing, err := s.srv.FindById(ctx, req.Ulid)
	if err != nil {
		return err
	}
	if existing == nil || existing.Ulid == "" || existing.DeletedAt != 0 {
		return apierror.ErrNotFound.WithMessage("skill not found")
	}
	if existing.IsSystem {
		return apierror.ErrForbidden.WithMessage("系统内置技能不能删除")
	}
	return s.srv.Delete(ctx, s.asm.D2EDelete(req))
}

func (s *SysSkillService) UpdateSysSkill(ctx context.Context, req *dto.UpdateSysSkillReq) error {
	en := s.asm.D2EUpdate(req)
	return s.srv.Update(ctx, en)
}

func (s *SysSkillService) FindSysSkillById(ctx context.Context, req *dto.FindSysSkillByIdReq) (*dto.FindSysSkillRsp, error) {
	en, err := s.srv.FindById(ctx, req.Ulid)
	if err != nil {
		return nil, err
	}
	if en == nil || en.Ulid == "" || en.DeletedAt != 0 {
		return nil, apierror.ErrNotFound.WithMessage("skill not found or deleted")
	}
	return s.asm.E2DFind(en), nil
}

func (s *SysSkillService) FindSysSkillAll(ctx context.Context, req *dto.FindSysSkillAllReq) ([]*dto.FindSysSkillRsp, error) {
	queries := []*query.Query{{Key: "deleted_at", Operator: query.OpEq, Value: 0}}
	if req.SkillType != "" {
		queries = append(queries, &query.Query{Key: "skill_type", Operator: query.OpEq, Value: req.SkillType})
	}
	if req.Name != "" {
		queries = append(queries, &query.Query{Key: "name", Operator: query.OpLikePercent, Value: req.Name})
	}
	ens, err := s.srv.FindAll(ctx, queries)
	if err != nil {
		return nil, err
	}
	return s.asm.E2DList(ens), nil
}

func (s *SysSkillService) FindSysSkillPage(ctx context.Context, req *dto.FindSysSkillPageReq) (*dto.FindSysSkillPageRsp, error) {
	ens, pageData, err := s.srv.FindPage(ctx, req.Query, req.PageData, req.SortData)
	if err != nil {
		return nil, err
	}
	return &dto.FindSysSkillPageRsp{Entries: s.asm.E2DList(ens), PageData: pageData}, nil
}

// CheckSkillName checks whether a non-deleted skill with name exists.
func (s *SysSkillService) CheckSkillName(ctx context.Context, name string) (*dto.CheckSkillNameRsp, error) {
	skill, err := s.srv.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	rsp := &dto.CheckSkillNameRsp{Exists: false, Message: ""}
	if skill != nil && skill.Ulid != "" && skill.DeletedAt == 0 {
		rsp.Exists = true
		rsp.Message = fmt.Sprintf("Skill '%s' 已存在，是否覆盖？", name)
	}
	return rsp, nil
}

// UploadSysSkill uploads and installs a skill ZIP package.
func (s *SysSkillService) UploadSysSkill(ctx context.Context, fileData []byte, fileName string, createdBy string) (*dto.FindSysSkillRsp, error) {
	skillsDir, skillInfo, err := s.extractSkillZip(fileData, fileName)
	if err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}

	existing, err := s.srv.FindByNameAndType(ctx, skillInfo.Name, skillInfo.SkillType)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.DeletedAt == 0 {
		return nil, apierror.ErrBadRequest.WithMessage(fmt.Sprintf("Skill '%s' (类型: %s) 已存在", skillInfo.Name, skillInfo.SkillType))
	}

	createReq := &dto.CreateSysSkillReq{
		CreatedBy:   createdBy,
		Name:        skillInfo.Name,
		Description: skillInfo.Description,
		SkillType:   skillInfo.SkillType,
		Version:     skillInfo.Version,
		Path:        skillsDir,
		Enabled:     true,
		Config:      "{}",
		IsSystem:    false,
		RiskLevel:   "low",
	}
	rsp, err := s.CreateSysSkill(ctx, createReq)
	if err != nil {
		return nil, err
	}
	return s.FindSysSkillById(ctx, &dto.FindSysSkillByIdReq{Ulid: rsp.Ulid})
}

// SkillMeta is parsed from SKILL.md front matter.
type SkillMeta struct {
	Name        string
	Description string
	SkillType   string
	Version     string
}

func (s *SysSkillService) extractSkillZip(fileData []byte, fileName string) (string, *SkillMeta, error) {
	reader, err := zip.NewReader(bytes.NewReader(fileData), int64(len(fileData)))
	if err != nil {
		return "", nil, fmt.Errorf("invalid zip file: %w", err)
	}

	skillRootDir := ""
	for _, f := range reader.File {
		name := filepath.ToSlash(f.Name)
		parts := strings.Split(strings.Trim(name, "/"), "/")
		if len(parts) > 0 && parts[0] != "" {
			skillRootDir = parts[0]
			break
		}
	}
	skillRootDir = pathClean(skillRootDir)
	if skillRootDir == "" {
		return "", nil, fmt.Errorf("invalid skill package: no root directory found")
	}

	targetDir := filepath.Join(s.skillsRoot, filepath.Clean(skillRootDir))
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", nil, fmt.Errorf("failed to create skill directory: %w", err)
	}
	for _, f := range reader.File {
		if err := s.extractFile(f, targetDir, skillRootDir); err != nil {
			return "", nil, err
		}
	}

	skillMeta, err := s.parseSkillMd(filepath.Join(targetDir, "SKILL.md"))
	if err != nil {
		skillMeta = &SkillMeta{Name: skillRootDir, SkillType: "skill", Version: "1.0.0"}
	}
	if skillMeta.Name == "" {
		skillMeta.Name = skillRootDir
	}
	if skillMeta.SkillType == "" {
		skillMeta.SkillType = "skill"
	}
	if skillMeta.Version == "" {
		skillMeta.Version = "1.0.0"
	}
	return targetDir, skillMeta, nil
}

func (s *SysSkillService) extractFile(f *zip.File, targetDir string, rootDir string) error {
	name := filepath.ToSlash(f.Name)
	clean := pathClean(name)
	if clean == "" {
		if strings.Trim(name, "/") == "" {
			return nil
		}
		return fmt.Errorf("invalid file path: %s", f.Name)
	}
	parts := strings.Split(clean, "/")
	if len(parts) > 0 && parts[0] == rootDir {
		clean = strings.Join(parts[1:], "/")
	}
	if clean == "" {
		return nil
	}
	filePath := filepath.Join(targetDir, filepath.FromSlash(clean))
	targetClean := filepath.Clean(targetDir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(filePath)+pathSuffix(f), targetClean) {
		return fmt.Errorf("invalid file path: %s", f.Name)
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(filePath, 0755)
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}
	dst, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer dst.Close()
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = io.Copy(dst, src)
	return err
}

func (s *SysSkillService) parseSkillMd(skillMdPath string) (*SkillMeta, error) {
	data, err := os.ReadFile(skillMdPath)
	if err != nil {
		return nil, err
	}
	content := string(data)
	meta := &SkillMeta{}
	if strings.HasPrefix(content, "---") {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) >= 3 {
			for _, line := range strings.Split(parts[1], "\n") {
				line = strings.TrimSpace(line)
				switch {
				case strings.HasPrefix(line, "name:"):
					meta.Name = unquoteMeta(strings.TrimSpace(strings.TrimPrefix(line, "name:")))
				case strings.HasPrefix(line, "description:"):
					meta.Description = unquoteMeta(strings.TrimSpace(strings.TrimPrefix(line, "description:")))
				case strings.HasPrefix(line, "type:"):
					meta.SkillType = unquoteMeta(strings.TrimSpace(strings.TrimPrefix(line, "type:")))
				case strings.HasPrefix(line, "version:"):
					meta.Version = unquoteMeta(strings.TrimSpace(strings.TrimPrefix(line, "version:")))
				}
			}
		}
	}
	return meta, nil
}

func defaultSkillsRoot() string {
	if dir := strings.TrimSpace(os.Getenv("SKILL_ROOT_DIR")); dir != "" {
		return dir
	}
	wd, err := os.Getwd()
	if err != nil {
		return filepath.Join(".", ".agent-runtime-client", "skills")
	}
	return filepath.Join(wd, ".agent-runtime-client", "skills")
}

func pathClean(name string) string {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || strings.HasPrefix(clean, "/") {
		return ""
	}
	return clean
}

func pathSuffix(f *zip.File) string {
	if f.FileInfo().IsDir() {
		return string(os.PathSeparator)
	}
	return ""
}

func unquoteMeta(v string) string {
	return strings.Trim(v, "'\"")
}
