package browsercredential

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/browser"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
)

const browserCommandTimeout = 90 * time.Second

var browserSessionPattern = regexp.MustCompile(`^athena-[a-f0-9]{32}$`)

type Service struct{ data *data.Data }

type SaveRequest struct {
	Name     string `json:"name"`
	LoginURL string `json:"login_url"`
	Username string `json:"username"`
	Password string `json:"password"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

type LoginRequest struct {
	SessionID string `json:"session_id"`
}

type Credential struct {
	Ulid           string `json:"ulid"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	Name           string `json:"name"`
	Domain         string `json:"domain"`
	LoginURL       string `json:"login_url"`
	UsernameMasked string `json:"username_masked"`
	Enabled        bool   `json:"enabled"`
}

type LoginResult struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
	Domain    string `json:"domain"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

func NewService(d *data.Data) *Service { return &Service{data: d} }

func (s *Service) Create(ctx context.Context, userID string, req SaveRequest) (*Credential, error) {
	if strings.TrimSpace(req.Password) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("password is required")
	}
	item, username, err := buildCredential(userID, req, "")
	if err != nil {
		return nil, err
	}
	item.Ulid = ulid.New()
	item.VaultRef = "athena-cred-" + strings.ToLower(item.Ulid)
	if err := saveToVault(ctx, item.VaultRef, item.LoginURL, username, req.Password); err != nil {
		return nil, err
	}
	if err := s.data.DB(ctx).Create(item).Error; err != nil {
		_ = deleteFromVault(context.Background(), item.VaultRef)
		return nil, fmt.Errorf("create website credential: %w", err)
	}
	return response(item), nil
}

func (s *Service) Update(ctx context.Context, userID, id string, req SaveRequest) (*Credential, error) {
	item, err := s.findOwned(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	loginURL := item.LoginURL
	if strings.TrimSpace(req.LoginURL) != "" {
		loginURL = req.LoginURL
	}
	parsed, err := validateLoginURL(loginURL)
	if err != nil {
		return nil, err
	}
	username := strings.TrimSpace(req.Username)
	if loginURL != item.LoginURL && strings.TrimSpace(req.Password) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("username and password are required when changing login_url")
	}
	if strings.TrimSpace(req.Password) != "" {
		if username == "" {
			return nil, apierror.ErrBadRequest.WithMessage("username is required when updating the password")
		}
		if err := saveToVault(ctx, item.VaultRef, parsed.String(), username, req.Password); err != nil {
			return nil, err
		}
		item.UsernameMasked = maskUsername(username)
	}
	if strings.TrimSpace(req.Name) != "" {
		item.Name = strings.TrimSpace(req.Name)
	}
	item.LoginURL = parsed.String()
	item.Domain = normalizeDomain(parsed.Hostname())
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	}
	if err := s.data.DB(ctx).Save(item).Error; err != nil {
		return nil, fmt.Errorf("update website credential: %w", err)
	}
	return response(item), nil
}

func (s *Service) List(ctx context.Context, userID, domain string) ([]Credential, error) {
	db := s.data.DB(ctx).Where("user_id = ? AND deleted_at = 0", userID)
	if normalized := normalizeDomain(domain); normalized != "" {
		db = db.Where("domain = ?", normalized)
	}
	var items []po.SiteCredential
	if err := db.Order("domain asc, name asc").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list website credentials: %w", err)
	}
	out := make([]Credential, 0, len(items))
	for i := range items {
		out = append(out, *response(&items[i]))
	}
	return out, nil
}

func (s *Service) Delete(ctx context.Context, userID, id string) error {
	item, err := s.findOwned(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := deleteFromVault(ctx, item.VaultRef); err != nil {
		return err
	}
	return s.data.DB(ctx).Model(&po.SiteCredential{}).
		Where("ulid = ? AND user_id = ? AND deleted_at = 0", id, userID).
		Updates(map[string]any{"deleted_at": time.Now().UnixMilli(), "enabled": false}).Error
}

func (s *Service) Login(ctx context.Context, userID, id string, req LoginRequest) (*LoginResult, error) {
	item, err := s.findOwned(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if !item.Enabled {
		return nil, apierror.ErrBadRequest.WithMessage("website credential is disabled")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID, err = newSessionID()
		if err != nil {
			return nil, fmt.Errorf("create browser session: %w", err)
		}
	}
	if !browserSessionPattern.MatchString(sessionID) {
		return nil, apierror.ErrBadRequest.WithMessage("invalid browser session_id")
	}
	// A failed auth login commonly means CAPTCHA/QR/2FA is now visible. Keep the
	// headed session open and hand control to the user instead of exposing secrets.
	_, loginErr := runAgentBrowser(ctx, nil, "--session", sessionID, "--headed", "auth", "login", item.VaultRef)
	status := "verification_required"
	message := "Credentials were filled. Confirm the result in the browser; complete CAPTCHA, QR, or 2FA manually if requested."
	if loginErr != nil {
		if _, openErr := runAgentBrowser(ctx, nil, "--session", sessionID, "open", item.LoginURL, "--headed"); openErr != nil {
			return nil, fmt.Errorf("start website login: %w", loginErr)
		}
		message = "Automatic sign-in could not finish. Continue in the opened browser and complete any required verification."
	}
	return &LoginResult{SessionID: sessionID, URL: item.LoginURL, Domain: item.Domain, Status: status, Message: message}, nil
}

func (s *Service) findOwned(ctx context.Context, userID, id string) (*po.SiteCredential, error) {
	var item po.SiteCredential
	if err := s.data.DB(ctx).Where("ulid = ? AND user_id = ? AND deleted_at = 0", id, userID).First(&item).Error; err != nil {
		return nil, apierror.ErrNotFound.WithMessage("website credential not found")
	}
	return &item, nil
}

func buildCredential(userID string, req SaveRequest, id string) (*po.SiteCredential, string, error) {
	parsed, err := validateLoginURL(req.LoginURL)
	if err != nil {
		return nil, "", err
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(userID) == "" {
		return nil, "", apierror.ErrBadRequest.WithMessage("name, username, and authenticated user are required")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return &po.SiteCredential{Ulid: id, UserID: userID, Name: strings.TrimSpace(req.Name), Domain: normalizeDomain(parsed.Hostname()), LoginURL: parsed.String(), UsernameMasked: maskUsername(username), Enabled: enabled}, username, nil
}

func validateLoginURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return nil, apierror.ErrBadRequest.WithMessage("login_url must be an absolute HTTP(S) URL without embedded credentials")
	}
	return parsed, nil
}

func normalizeDomain(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if parsed, err := url.Parse(raw); err == nil && parsed.Hostname() != "" {
		return strings.TrimPrefix(parsed.Hostname(), "www.")
	}
	return strings.TrimPrefix(raw, "www.")
}

func maskUsername(username string) string {
	if at := strings.Index(username, "@"); at > 0 {
		name, domain := username[:at], username[at:]
		if len(name) <= 2 {
			return name[:1] + "***" + domain
		}
		return name[:2] + "***" + domain
	}
	chars := []rune(username)
	if len(chars) <= 2 {
		return string(chars[:1]) + "***"
	}
	return string(chars[:2]) + "***" + string(chars[len(chars)-1:])
}

func response(item *po.SiteCredential) *Credential {
	return &Credential{Ulid: item.Ulid, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, Name: item.Name, Domain: item.Domain, LoginURL: item.LoginURL, UsernameMasked: item.UsernameMasked, Enabled: item.Enabled}
}

func saveToVault(ctx context.Context, vaultRef, loginURL, username, password string) error {
	_, err := runAgentBrowser(ctx, []byte(password), "auth", "save", vaultRef, "--url", loginURL, "--username", username, "--password-stdin")
	if err != nil {
		return fmt.Errorf("save website credential to Auth Vault: %w", err)
	}
	return nil
}

func deleteFromVault(ctx context.Context, vaultRef string) error {
	if _, err := runAgentBrowser(ctx, nil, "auth", "delete", vaultRef); err != nil {
		return fmt.Errorf("delete website credential from Auth Vault: %w", err)
	}
	return nil
}

func runAgentBrowser(ctx context.Context, stdin []byte, args ...string) (string, error) {
	name := strings.TrimSpace(os.Getenv("ATHENA_AGENT_BROWSER_BIN"))
	if name == "" {
		path, err := exec.LookPath("agent-browser")
		if err != nil {
			return "", apierror.ErrRuntimeUnavailable.WithMessage("agent-browser is not installed")
		}
		name = path
	}
	commandCtx, cancel := context.WithTimeout(ctx, browserCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, name, args...)
	cmd.Env = os.Environ()
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(append(stdin, '\n'))
	}
	output, err := cmd.CombinedOutput()
	if commandCtx.Err() == context.DeadlineExceeded {
		return "", apierror.ErrTimeout.WithMessage("agent-browser command timed out")
	}
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if len(detail) > 1000 {
			detail = detail[:1000]
		}
		return "", fmt.Errorf("agent-browser command failed: %w: %s", err, detail)
	}
	return strings.TrimSpace(string(output)), nil
}

func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "athena-" + hex.EncodeToString(b), nil
}
