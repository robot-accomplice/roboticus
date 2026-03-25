package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// BrowserConfig holds browser automation configuration.
type BrowserConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	Headless       bool   `mapstructure:"headless"`        // default true
	CDPPort        int    `mapstructure:"cdp_port"`        // default 9222
	ExecutablePath string `mapstructure:"executable_path"` // custom chromium binary
	TimeoutSeconds int    `mapstructure:"timeout_seconds"` // default 30
}

// ActionKind enumerates browser actions.
type ActionKind string

const (
	ActionNavigate     ActionKind = "navigate"
	ActionClick        ActionKind = "click"
	ActionType         ActionKind = "type"
	ActionScreenshot   ActionKind = "screenshot"
	ActionPDF          ActionKind = "pdf"
	ActionEvaluate     ActionKind = "evaluate"
	ActionGetCookies   ActionKind = "get_cookies"
	ActionClearCookies ActionKind = "clear_cookies"
	ActionReadPage     ActionKind = "read_page"
	ActionGoBack       ActionKind = "go_back"
	ActionGoForward    ActionKind = "go_forward"
	ActionReload       ActionKind = "reload"
)

// BrowserAction represents an action to perform in the browser.
type BrowserAction struct {
	Kind     ActionKind `json:"kind"`
	URL      string     `json:"url,omitempty"`
	Selector string     `json:"selector,omitempty"`
	Text     string     `json:"text,omitempty"`
	Script   string     `json:"script,omitempty"`
}

// ActionResult contains the outcome of a browser action.
type ActionResult struct {
	Success bool            `json:"success"`
	Action  ActionKind      `json:"action"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// PageInfo holds basic page metadata.
type PageInfo struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

// ScreenshotResult holds screenshot data.
type ScreenshotResult struct {
	DataBase64 string `json:"data_base64"`
	Format     string `json:"format"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
}

// PageContent holds extracted page content.
type PageContent struct {
	URL        string `json:"url"`
	Title      string `json:"title"`
	Text       string `json:"text"`
	HTMLLength int    `json:"html_length"`
}

// Browser manages a headless Chromium process and CDP session.
type Browser struct {
	cfg     BrowserConfig
	mu      sync.Mutex
	process *exec.Cmd
	client  *http.Client
	running bool
	session *CdpSession
}

// NewBrowser creates a browser automation instance.
func NewBrowser(cfg BrowserConfig) *Browser {
	if cfg.CDPPort == 0 {
		cfg.CDPPort = 9222
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = 30
	}
	return &Browser{
		cfg: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
		},
	}
}

// Start launches the Chromium process with CDP enabled.
func (b *Browser) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return nil
	}

	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = detectChromium()
	}
	if execPath == "" {
		return fmt.Errorf("browser: chromium not found")
	}

	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", b.cfg.CDPPort),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-sync",
	}
	if b.cfg.Headless {
		args = append(args, "--headless=new")
	}

	b.process = exec.Command(execPath, args...)
	if err := b.process.Start(); err != nil {
		return fmt.Errorf("browser: start: %w", err)
	}

	// Wait for CDP to be ready.
	if err := b.waitForCDP(5 * time.Second); err != nil {
		b.process.Process.Kill()
		return fmt.Errorf("browser: CDP not ready: %w", err)
	}

	b.running = true

	// Connect CDP WebSocket session to the first page target.
	target, err := FindPageTarget(b.cdpURL(""))
	if err == nil && target.WebSocketDebuggerURL != "" {
		timeout := time.Duration(b.cfg.TimeoutSeconds) * time.Second
		sess, sessErr := ConnectCdp(context.Background(), target.WebSocketDebuggerURL, timeout)
		if sessErr == nil {
			b.session = sess
			// Enable required domains.
			ctx := context.Background()
			sess.SendCommand(ctx, "Page.enable", nil)
			sess.SendCommand(ctx, "Runtime.enable", nil)
			sess.SendCommand(ctx, "Network.enable", nil)
		}
	}

	log.Info().Int("port", b.cfg.CDPPort).Msg("browser started")
	return nil
}

// Stop terminates the browser process.
func (b *Browser) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return nil
	}

	if b.session != nil {
		b.session.Close()
		b.session = nil
	}
	if b.process != nil && b.process.Process != nil {
		b.process.Process.Kill()
		b.process.Wait()
	}
	b.running = false
	log.Info().Msg("browser stopped")
	return nil
}

// IsRunning returns whether the browser is active.
func (b *Browser) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

// Execute performs a browser action via CDP.
func (b *Browser) Execute(ctx context.Context, action *BrowserAction) ActionResult {
	b.mu.Lock()
	running := b.running
	b.mu.Unlock()

	if !running {
		return ActionResult{
			Action:  action.Kind,
			Success: false,
			Error:   "browser not running",
		}
	}

	result := b.executeAction(ctx, action)

	// Session recovery for idempotent actions.
	if !result.Success && isRecoverable(result.Error) && isIdempotent(action.Kind) {
		log.Debug().Str("action", string(action.Kind)).Msg("browser: attempting recovery")
		if err := b.recover(); err == nil {
			result = b.executeAction(ctx, action)
		}
	}

	return result
}

func (b *Browser) executeAction(ctx context.Context, action *BrowserAction) ActionResult {
	// CDP command dispatch.
	switch action.Kind {
	case ActionNavigate:
		return b.cdpNavigate(ctx, action.URL)
	case ActionEvaluate:
		return b.cdpEvaluate(ctx, action.Script)
	case ActionScreenshot:
		return b.cdpScreenshot(ctx)
	case ActionReadPage:
		return b.cdpReadPage(ctx)
	case ActionGoBack:
		return b.cdpSimpleCommand(ctx, "Page.goBack", action.Kind)
	case ActionGoForward:
		return b.cdpSimpleCommand(ctx, "Page.goForward", action.Kind)
	case ActionReload:
		return b.cdpSimpleCommand(ctx, "Page.reload", action.Kind)
	case ActionGetCookies:
		return b.cdpGetCookies(ctx)
	case ActionClearCookies:
		return b.cdpSimpleCommand(ctx, "Network.clearBrowserCookies", action.Kind)
	case ActionClick:
		script := fmt.Sprintf(`document.querySelector(%q).click()`, action.Selector)
		return b.cdpEvaluate(ctx, script)
	case ActionType:
		script := fmt.Sprintf(`(function(){var el=document.querySelector(%q);el.value=%q;el.dispatchEvent(new Event('input'))})()`, action.Selector, action.Text)
		return b.cdpEvaluate(ctx, script)
	default:
		return ActionResult{Action: action.Kind, Error: "unknown action"}
	}
}

func (b *Browser) cdpURL(path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", b.cfg.CDPPort, path)
}

func (b *Browser) cdpNavigate(ctx context.Context, url string) ActionResult {
	if b.session == nil {
		return ActionResult{Action: ActionNavigate, Error: "no CDP session"}
	}
	result, err := b.session.SendCommand(ctx, "Page.navigate", map[string]string{"url": url})
	if err != nil {
		return ActionResult{Action: ActionNavigate, Error: err.Error()}
	}
	return ActionResult{Action: ActionNavigate, Success: true, Data: result}
}

func (b *Browser) cdpEvaluate(ctx context.Context, script string) ActionResult {
	if b.session == nil {
		return ActionResult{Action: ActionEvaluate, Error: "no CDP session"}
	}
	result, err := b.session.SendCommand(ctx, "Runtime.evaluate", map[string]any{
		"expression":    script,
		"returnByValue": true,
	})
	if err != nil {
		return ActionResult{Action: ActionEvaluate, Error: err.Error()}
	}
	return ActionResult{Action: ActionEvaluate, Success: true, Data: result}
}

func (b *Browser) cdpScreenshot(ctx context.Context) ActionResult {
	if b.session == nil {
		return ActionResult{Action: ActionScreenshot, Error: "no CDP session"}
	}
	result, err := b.session.SendCommand(ctx, "Page.captureScreenshot", map[string]string{"format": "png"})
	if err != nil {
		return ActionResult{Action: ActionScreenshot, Error: err.Error()}
	}
	return ActionResult{Action: ActionScreenshot, Success: true, Data: result}
}

func (b *Browser) cdpReadPage(ctx context.Context) ActionResult {
	return b.cdpEvaluate(ctx, `({url: document.URL, title: document.title, text: document.body.innerText, htmlLength: document.documentElement.outerHTML.length})`)
}

func (b *Browser) cdpGetCookies(ctx context.Context) ActionResult {
	if b.session == nil {
		return ActionResult{Action: ActionGetCookies, Error: "no CDP session"}
	}
	result, err := b.session.SendCommand(ctx, "Network.getCookies", nil)
	if err != nil {
		return ActionResult{Action: ActionGetCookies, Error: err.Error()}
	}
	return ActionResult{Action: ActionGetCookies, Success: true, Data: result}
}

func (b *Browser) cdpSimpleCommand(ctx context.Context, method string, kind ActionKind) ActionResult {
	if b.session == nil {
		return ActionResult{Action: kind, Error: "no CDP session"}
	}
	_, err := b.session.SendCommand(ctx, method, nil)
	if err != nil {
		return ActionResult{Action: kind, Error: err.Error()}
	}
	return ActionResult{Action: kind, Success: true}
}

// ListTargets returns all CDP targets (tabs).
func (b *Browser) ListTargets() ([]PageInfo, error) {
	resp, err := b.client.Get(b.cdpURL("/json/list"))
	if err != nil {
		return nil, fmt.Errorf("browser: list targets: %w", err)
	}
	defer resp.Body.Close()

	var targets []struct {
		ID    string `json:"id"`
		URL   string `json:"url"`
		Title string `json:"title"`
		Type  string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, err
	}

	var pages []PageInfo
	for _, t := range targets {
		if t.Type == "page" {
			pages = append(pages, PageInfo{ID: t.ID, URL: t.URL, Title: t.Title})
		}
	}
	return pages, nil
}

func (b *Browser) waitForCDP(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := b.client.Get(b.cdpURL("/json/version"))
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for CDP")
}

func (b *Browser) recover() error {
	b.Stop()
	return b.Start()
}

func isRecoverable(errMsg string) bool {
	return errMsg == "websocket closed" || errMsg == "broken pipe" || errMsg == "connection refused"
}

func isIdempotent(kind ActionKind) bool {
	switch kind {
	case ActionNavigate, ActionScreenshot, ActionPDF, ActionEvaluate,
		ActionGetCookies, ActionReadPage, ActionGoBack, ActionGoForward, ActionReload:
		return true
	default:
		return false
	}
}

// detectChromium finds Chromium/Chrome on the system.
func detectChromium() string {
	candidates := []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	}
	if runtime.GOOS == "windows" {
		candidates = append(candidates,
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		)
	}
	for _, c := range candidates {
		if path, err := exec.LookPath(c); err == nil {
			return path
		}
	}
	return ""
}
