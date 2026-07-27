package workspace

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

const (
	maxTreeEntries      = 1200
	maxReadBytes        = 256 * 1024
	maxSearchHits       = 100
	maxContextFiles     = 6
	maxContextBytes     = 48 * 1024
	maxContextFileBytes = 16 * 1024
)

var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "build": true,
	".next": true, ".turbo": true, "vendor": true, "__pycache__": true,
}

type Handler struct {
	mu         sync.RWMutex
	workspaces map[string]workspace
}

type workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Root string `json:"root"`
}

type treeNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	Type     string     `json:"type"`
	Size     int64      `json:"size,omitempty"`
	Children []treeNode `json:"children,omitempty"`
}

type importReq struct {
	Path string `json:"path"`
}

type searchReq struct {
	Query string `json:"query"`
}

type patchReq struct {
	Patch  string `json:"patch"`
	DryRun bool   `json:"dry_run"`
}

type buildPatchReq struct {
	Changes []patchChange `json:"changes"`
}

type patchChange struct {
	Path       string `json:"path"`
	Find       string `json:"find"`
	Replace    string `json:"replace"`
	Occurrence int    `json:"occurrence,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	EndLine    int    `json:"end_line,omitempty"`
}

type contextReq struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type searchHit struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Preview string `json:"preview"`
}

type contextFile struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	Score     int    `json:"score"`
	StartLine int    `json:"start_line"`
	LineCount int    `json:"line_count"`
}

func NewHandler() *Handler {
	return &Handler{workspaces: map[string]workspace{}}
}

func (h *Handler) SelectFolder(c *gin.Context) {
	path, err := selectFolder()
	if err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	response.Ok(c, gin.H{"path": path})
}

func (h *Handler) Import(c *gin.Context) {
	var req importReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	root, err := filepath.Abs(strings.TrimSpace(req.Path))
	if err != nil || root == "" {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("invalid path"))
		return
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("path must be an existing directory"))
		return
	}
	ws := workspace{ID: workspaceID(root), Name: filepath.Base(root), Root: root}
	h.mu.Lock()
	h.workspaces[ws.ID] = ws
	h.mu.Unlock()
	response.Ok(c, ws)
}

func selectFolder() (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("osascript", "-e", `POSIX path of (choose folder with prompt "选择要导入的项目目录")`)
	case "windows":
		script := `Add-Type -AssemblyName System.Windows.Forms; $d = New-Object System.Windows.Forms.FolderBrowserDialog; if ($d.ShowDialog() -eq "OK") { $d.SelectedPath }`
		cmd = exec.Command("powershell", "-NoProfile", "-Command", script)
	case "linux":
		if _, err := exec.LookPath("zenity"); err == nil {
			cmd = exec.Command("zenity", "--file-selection", "--directory", "--title=选择要导入的项目目录")
		} else if _, err := exec.LookPath("kdialog"); err == nil {
			cmd = exec.Command("kdialog", "--getexistingdirectory", ".")
		}
	}
	if cmd == nil {
		return "", os.ErrInvalid
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = "folder picker is unavailable or canceled"
		}
		return "", apierror.ErrBadRequest.WithMessage(msg)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", apierror.ErrBadRequest.WithMessage("folder selection canceled")
	}
	return path, nil
}

func (h *Handler) Tree(c *gin.Context) {
	ws, ok := h.workspace(c)
	if !ok {
		return
	}
	var count int
	node, err := h.buildTree(ws, strings.TrimSpace(c.Query("path")), &count)
	if err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	response.Ok(c, gin.H{"root": ws, "tree": node, "truncated": count >= maxTreeEntries})
}

func (h *Handler) ReadFile(c *gin.Context) {
	ws, ok := h.workspace(c)
	if !ok {
		return
	}
	abs, rel, err := safePath(ws.Root, c.Query("path"))
	if err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("path must be a file"))
		return
	}
	if info.Size() > maxReadBytes {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("file too large to preview"))
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		_ = c.Error(apierror.ErrInternal.WithMessage(err.Error()))
		return
	}
	content := string(data)
	response.Ok(c, gin.H{"path": rel, "content": content, "size": info.Size(), "line_count": textLineCount(content)})
}

func (h *Handler) Search(c *gin.Context) {
	ws, ok := h.workspace(c)
	if !ok {
		return
	}
	var req searchReq
	_ = c.ShouldBindJSON(&req)
	query := strings.TrimSpace(req.Query)
	if query == "" {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("query is required"))
		return
	}
	hits := make([]searchHit, 0)
	_ = filepath.WalkDir(ws.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(hits) >= maxSearchHits {
			return nil
		}
		if d.IsDir() {
			if ignoredDirs[d.Name()] && path != ws.Root {
				return filepath.SkipDir
			}
			return nil
		}
		if !isTextLike(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxReadBytes {
			return nil
		}
		scanFile(path, ws.Root, query, &hits)
		return nil
	})
	response.Ok(c, gin.H{"hits": hits})
}

func (h *Handler) Context(c *gin.Context) {
	ws, ok := h.workspace(c)
	if !ok {
		return
	}
	var req contextReq
	_ = c.ShouldBindJSON(&req)
	query := strings.TrimSpace(req.Query)
	if query == "" {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("query is required"))
		return
	}
	limit := req.Limit
	if limit <= 0 || limit > maxContextFiles {
		limit = maxContextFiles
	}
	files := collectContextFiles(ws.Root, query, limit)
	response.Ok(c, gin.H{"files": files})
}

func (h *Handler) BuildPatch(c *gin.Context) {
	ws, ok := h.workspace(c)
	if !ok {
		return
	}
	var req buildPatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	patch, err := buildWorkspacePatch(ws.Root, req.Changes)
	if err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	response.Ok(c, gin.H{"patch": patch})
}

func (h *Handler) ApplyPatch(c *gin.Context) {
	ws, ok := h.workspace(c)
	if !ok {
		return
	}
	var req patchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	patch := normalizePatch(req.Patch)
	if strings.TrimSpace(patch) == "" {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("patch is required"))
		return
	}
	if err := gitApply(ws.Root, patch, true); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if !req.DryRun {
		if err := gitApply(ws.Root, patch, false); err != nil {
			_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
			return
		}
	}
	response.Ok(c, gin.H{"applied": !req.DryRun})
}

func normalizePatch(patch string) string {
	patch = strings.TrimSpace(patch)
	if strings.HasPrefix(patch, "```") {
		lines := strings.Split(patch, "\n")
		if len(lines) > 1 {
			lines = lines[1:]
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
				lines = lines[:len(lines)-1]
			}
			patch = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	markers := []string{"diff --git ", "--- a/", "--- "}
	start := -1
	for _, marker := range markers {
		if idx := strings.Index(patch, marker); idx >= 0 && (start == -1 || idx < start) {
			start = idx
		}
	}
	if start > 0 {
		patch = patch[start:]
	}
	if idx := strings.LastIndex(patch, "\n```"); idx >= 0 {
		patch = patch[:idx]
	}
	patch = strings.TrimSpace(patch)
	if patch == "" {
		return ""
	}
	return patch + "\n"
}

func buildWorkspacePatch(root string, changes []patchChange) (string, error) {
	if len(changes) == 0 {
		return "", fmt.Errorf("changes are required")
	}
	if len(changes) > 20 {
		return "", fmt.Errorf("too many changes: maximum is 20")
	}
	original := make(map[string]string)
	updated := make(map[string]string)
	ordered := append([]patchChange(nil), changes...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Path == ordered[j].Path && ordered[i].StartLine > 0 && ordered[j].StartLine > 0 {
			return ordered[i].StartLine > ordered[j].StartLine
		}
		return false
	})
	for i, change := range ordered {
		if strings.TrimSpace(change.Path) == "" || (change.StartLine <= 0 && change.Find == "") {
			return "", fmt.Errorf("change %d requires path and either start_line or find", i+1)
		}
		abs, rel, err := safePath(root, change.Path)
		if err != nil {
			return "", fmt.Errorf("change %d has invalid path", i+1)
		}
		content, exists := updated[rel]
		if !exists {
			info, err := os.Stat(abs)
			if err != nil || info.IsDir() {
				return "", fmt.Errorf("change %d path must be an existing file: %s", i+1, rel)
			}
			if info.Size() > maxReadBytes {
				return "", fmt.Errorf("change %d file is too large: %s", i+1, rel)
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				return "", fmt.Errorf("read %s: %w", rel, err)
			}
			content = string(data)
			original[rel] = content
		}
		if change.StartLine > 0 {
			endLine := change.EndLine
			if endLine == 0 {
				endLine = change.StartLine
			}
			next, err := replaceLineRange(content, change.StartLine, endLine, change.Replace)
			if err != nil {
				return "", fmt.Errorf("change %d in %s: %w", i+1, rel, err)
			}
			updated[rel] = next
			continue
		}
		matches := strings.Count(content, change.Find)
		if matches == 0 {
			return "", fmt.Errorf("change %d find text matched 0 times in %s", i+1, rel)
		}
		if matches > 1 && change.Occurrence == 0 {
			lines := findMatchLines(content, change.Find, 8)
			location := ""
			if len(lines) > 0 {
				location = fmt.Sprintf(" at lines %v", lines)
			}
			return "", fmt.Errorf("change %d find text matched %d times%s in %s; add occurrence (1-%d) or use more unique find text", i+1, matches, location, rel, matches)
		}
		occurrence := change.Occurrence
		if occurrence == 0 {
			occurrence = 1
		}
		if occurrence > matches {
			return "", fmt.Errorf("change %d occurrence %d exceeds %d matches in %s", i+1, occurrence, matches, rel)
		}
		updated[rel] = replaceOccurrence(content, change.Find, change.Replace, occurrence)
	}

	paths := make([]string, 0, len(updated))
	for path := range updated {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var patch strings.Builder
	for _, path := range paths {
		diff, err := unifiedFileDiff(path, original[path], updated[path])
		if err != nil {
			return "", err
		}
		patch.WriteString(diff)
	}
	if patch.Len() == 0 {
		return "", fmt.Errorf("changes do not modify any file")
	}
	return patch.String(), nil
}

func findMatchLines(content, find string, limit int) []int {
	if find == "" || limit <= 0 {
		return nil
	}
	lines := make([]int, 0, limit)
	for offset := 0; offset <= len(content)-len(find) && len(lines) < limit; {
		index := strings.Index(content[offset:], find)
		if index < 0 {
			break
		}
		absolute := offset + index
		lines = append(lines, strings.Count(content[:absolute], "\n")+1)
		offset = absolute + len(find)
	}
	return lines
}

func replaceOccurrence(content, find, replacement string, occurrence int) string {
	if occurrence <= 0 {
		return content
	}
	offset := 0
	for current := 1; current <= occurrence; current++ {
		index := strings.Index(content[offset:], find)
		if index < 0 {
			return content
		}
		absolute := offset + index
		if current == occurrence {
			return content[:absolute] + replacement + content[absolute+len(find):]
		}
		offset = absolute + len(find)
	}
	return content
}

func replaceLineRange(content string, startLine, endLine int, replacement string) (string, error) {
	if startLine <= 0 || endLine < startLine {
		return "", fmt.Errorf("invalid line range %d-%d", startLine, endLine)
	}
	if content == "" {
		return "", fmt.Errorf("line range %d-%d exceeds empty file", startLine, endLine)
	}
	starts := []int{0}
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' && i+1 < len(content) {
			starts = append(starts, i+1)
		}
	}
	lineCount := len(starts)
	if startLine == lineCount+1 {
		if replacement == "" {
			return content, nil
		}
		separator := ""
		if content != "" && !strings.HasSuffix(content, "\n") {
			separator = "\n"
		}
		if strings.HasSuffix(content, "\n") && !strings.HasSuffix(replacement, "\n") {
			replacement += "\n"
		}
		return content + separator + replacement, nil
	}
	if endLine > len(starts) {
		return "", fmt.Errorf("line range %d-%d exceeds file line count %d", startLine, endLine, len(starts))
	}
	startOffset := starts[startLine-1]
	endOffset := len(content)
	if endLine < len(starts) {
		endOffset = starts[endLine]
	}
	if replacement != "" && endOffset < len(content) && !strings.HasSuffix(replacement, "\n") {
		replacement += "\n"
	}
	if replacement != "" && endOffset == len(content) && strings.HasSuffix(content, "\n") && !strings.HasSuffix(replacement, "\n") {
		replacement += "\n"
	}
	return content[:startOffset] + replacement + content[endOffset:], nil
}

func textLineCount(content string) int {
	if content == "" {
		return 0
	}
	count := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		count++
	}
	return count
}

func unifiedFileDiff(rel, before, after string) (string, error) {
	tempDir, err := os.MkdirTemp("", "arc-workspace-diff-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)
	oldPath := filepath.Join(tempDir, "old")
	newPath := filepath.Join(tempDir, "new")
	if err := os.WriteFile(oldPath, []byte(before), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(newPath, []byte(after), 0o600); err != nil {
		return "", err
	}
	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--", oldPath, newPath)
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		return "", nil
	}
	exitErr, ok := runErr.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		return "", fmt.Errorf("generate diff for %s: %s", rel, strings.TrimSpace(string(out)))
	}
	text := string(out)
	hunkStart := strings.Index(text, "@@ ")
	if hunkStart < 0 {
		return "", fmt.Errorf("generate diff for %s: no hunk produced", rel)
	}
	hunks := strings.TrimLeft(text[hunkStart:], "\n")
	if !strings.HasSuffix(hunks, "\n") {
		hunks += "\n"
	}
	return fmt.Sprintf("diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n%s", rel, rel, rel, rel, hunks), nil
}

func collectContextFiles(root, query string, limit int) []contextFile {
	tokens := queryTokens(query)
	candidates := make([]contextFile, 0)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if ignoredDirs[d.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !isTextLike(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxReadBytes {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		content := string(data)
		score := contextScore(filepath.ToSlash(rel), content, tokens)
		if score > 0 {
			excerpt, startLine := contextExcerptWithLine(content, tokens, maxContextFileBytes)
			candidates = append(candidates, contextFile{
				Path:      filepath.ToSlash(rel),
				Content:   excerpt,
				Size:      info.Size(),
				Score:     score,
				StartLine: startLine,
				LineCount: textLineCount(content),
			})
		}
		return nil
	})
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Path < candidates[j].Path
	})
	candidates = preferNamedContextFiles(candidates, query)
	total := 0
	out := make([]contextFile, 0, limit)
	for _, file := range candidates {
		if len(out) >= limit || total >= maxContextBytes {
			break
		}
		content := file.Content
		if total+len(content) > maxContextBytes {
			content = content[:maxContextBytes-total]
		}
		file.Content = content
		out = append(out, file)
		total += len(content)
	}
	return out
}

func preferNamedContextFiles(candidates []contextFile, query string) []contextFile {
	query = strings.ToLower(query)
	named := make([]contextFile, 0, len(candidates))
	for _, file := range candidates {
		path := strings.ToLower(file.Path)
		base := strings.ToLower(filepath.Base(file.Path))
		if strings.Contains(query, path) || (len(base) >= 4 && strings.Contains(query, base)) {
			named = append(named, file)
		}
	}
	if len(named) > 0 {
		return named
	}
	return candidates
}

func contextExcerpt(content string, tokens []string, limit int) string {
	excerpt, _ := contextExcerptWithLine(content, tokens, limit)
	return excerpt
}

func contextExcerptWithLine(content string, tokens []string, limit int) (string, int) {
	if limit <= 0 || len(content) <= limit {
		return content, 1
	}
	lower := strings.ToLower(content)
	match := -1
	for _, token := range tokens {
		if idx := strings.Index(lower, token); idx >= 0 && (match == -1 || idx < match) {
			match = idx
		}
	}
	if match < 0 {
		match = 0
	}
	start := match - limit/3
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(content) {
		end = len(content)
		start = end - limit
	}
	if start > 0 {
		if nextLine := strings.IndexByte(content[start:end], '\n'); nextLine >= 0 {
			start += nextLine + 1
		}
	}
	if end < len(content) {
		if lastLine := strings.LastIndexByte(content[start:end], '\n'); lastLine >= 0 {
			end = start + lastLine + 1
		}
	}
	startLine := strings.Count(content[:start], "\n") + 1
	return content[start:end], startLine
}

func queryTokens(query string) []string {
	raw := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' && r != '/'
	})
	tokens := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, token := range raw {
		token = strings.Trim(token, "/_-")
		if len(token) < 2 || seen[token] {
			continue
		}
		seen[token] = true
		tokens = append(tokens, token)
	}
	if len(tokens) == 0 && strings.TrimSpace(query) != "" {
		tokens = append(tokens, strings.ToLower(strings.TrimSpace(query)))
	}
	return tokens
}

func contextScore(rel, content string, tokens []string) int {
	relLower := strings.ToLower(rel)
	contentLower := strings.ToLower(content)
	score := 0
	for _, token := range tokens {
		if strings.Contains(relLower, token) {
			score += 8
		}
		if strings.Contains(contentLower, token) {
			score += 2
		}
	}
	if strings.HasSuffix(relLower, "package.json") || strings.HasSuffix(relLower, "go.mod") || strings.HasSuffix(relLower, "readme.md") {
		score++
	}
	return score
}

func (h *Handler) workspace(c *gin.Context) (workspace, bool) {
	id := strings.TrimSpace(c.Param("id"))
	h.mu.RLock()
	ws, ok := h.workspaces[id]
	h.mu.RUnlock()
	if !ok {
		_ = c.Error(apierror.ErrNotFound.WithMessage("workspace not found"))
		return workspace{}, false
	}
	return ws, true
}

func (h *Handler) buildTree(ws workspace, rel string, count *int) (treeNode, error) {
	abs, cleanRel, err := safePath(ws.Root, rel)
	if err != nil {
		return treeNode{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return treeNode{}, err
	}
	node := treeNode{Name: info.Name(), Path: cleanRel, Type: "file", Size: info.Size()}
	if info.IsDir() {
		node.Type = "dir"
		node.Size = 0
		entries, err := os.ReadDir(abs)
		if err != nil {
			return treeNode{}, err
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir() != entries[j].IsDir() {
				return entries[i].IsDir()
			}
			return entries[i].Name() < entries[j].Name()
		})
		for _, entry := range entries {
			if *count >= maxTreeEntries {
				break
			}
			if entry.IsDir() && ignoredDirs[entry.Name()] {
				continue
			}
			*count++
			childRel := filepath.ToSlash(filepath.Join(cleanRel, entry.Name()))
			child, err := h.buildTree(ws, childRel, count)
			if err == nil {
				node.Children = append(node.Children, child)
			}
		}
	}
	if cleanRel == "." {
		node.Name = ws.Name
		node.Path = ""
	}
	return node, nil
}

func workspaceID(root string) string {
	sum := sha1.Sum([]byte(root))
	return hex.EncodeToString(sum[:])[:16]
}

func safePath(root, rel string) (string, string, error) {
	clean := filepath.Clean(strings.TrimSpace(rel))
	if clean == "" || clean == "." {
		clean = "."
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", "", os.ErrPermission
	}
	abs := filepath.Join(root, clean)
	relToRoot, err := filepath.Rel(root, abs)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, "../") || filepath.IsAbs(relToRoot) {
		return "", "", os.ErrPermission
	}
	return abs, filepath.ToSlash(relToRoot), nil
}

func isTextLike(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".json", ".md", ".yml", ".yaml", ".css", ".html", ".py", ".rs", ".java", ".sql", ".sh", ".toml", ".xml":
		return true
	default:
		return ext == ""
	}
}

func scanFile(path, root, query string, hits *[]searchHit) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	lineNo := 0
	lowerQuery := strings.ToLower(query)
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if strings.Contains(strings.ToLower(line), lowerQuery) {
			rel, _ := filepath.Rel(root, path)
			*hits = append(*hits, searchHit{Path: filepath.ToSlash(rel), Line: lineNo, Preview: strings.TrimSpace(line)})
			if len(*hits) >= maxSearchHits {
				return
			}
		}
	}
}

func gitApply(root, patch string, check bool) error {
	args := []string{"apply", "--whitespace=nowarn"}
	if check {
		args = append(args, "--check")
	}
	args = append(args, "-")
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(patch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return apierror.ErrBadRequest.WithMessage(msg)
	}
	return nil
}
