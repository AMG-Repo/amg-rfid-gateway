package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/tui"
)

func TestMainBuildsBridgeServerWithConfiguredSocketPath(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse gateway main source: %v", err)
	}

	mainFunc := functionDecl(file, "main")
	if mainFunc == nil {
		t.Fatal("gateway source must declare main")
	}

	call := callTo(mainFunc.Body, "newBridgeServer")
	if call == nil {
		t.Fatal("main must construct the bridge through newBridgeServer, not direct tui.NewBridgeServer")
	}
	if len(call.Args) != 4 {
		t.Fatalf("newBridgeServer in main has %d arguments, want 4", len(call.Args))
	}
	if !isIdentifier(call.Args[0], "cfg") {
		t.Fatalf("newBridgeServer first argument = %s, want cfg", exprString(call.Args[0]))
	}
	if !isSelector(call.Args[3], "tui", "NewBridgeServer") {
		t.Fatalf("newBridgeServer constructor argument = %s, want tui.NewBridgeServer", exprString(call.Args[3]))
	}
}

func functionDecl(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if ok && funcDecl.Name.Name == name {
			return funcDecl
		}
	}
	return nil
}

func callTo(body *ast.BlockStmt, name string) *ast.CallExpr {
	var match *ast.CallExpr
	ast.Inspect(body, func(node ast.Node) bool {
		if match != nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if ok && isIdentifier(call.Fun, name) {
			match = call
		}
		return true
	})
	return match
}

func isIdentifier(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func isSelector(expr ast.Expr, packageName, selector string) bool {
	selectorExpr, ok := expr.(*ast.SelectorExpr)
	return ok && isIdentifier(selectorExpr.X, packageName) && selectorExpr.Sel.Name == selector
}

func exprString(expr ast.Expr) string {
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	if selector, ok := expr.(*ast.SelectorExpr); ok {
		return exprString(selector.X) + "." + selector.Sel.Name
	}
	return "unexpected expression"
}

func TestNewBridgeServerUsesConfiguredSocketPath(t *testing.T) {
	const configuredSocketPath = "/run/amg-rfid-gateway/bridge.sock"

	cfg := &config.GatewayConfig{SocketPath: configuredSocketPath}
	var receivedSocketPath string

	newBridgeServer(cfg, nil, nil, func(socketPath string, _ tui.HealthMonitor, _ tui.AntennaProvider) *tui.BridgeServer {
		receivedSocketPath = socketPath
		return nil
	})

	if receivedSocketPath != configuredSocketPath {
		t.Fatalf("bridge constructor socket path = %q, want %q", receivedSocketPath, configuredSocketPath)
	}
}

func TestNewBridgeServerUsesEffectiveDefaultSocketPath(t *testing.T) {
	cfg := &config.GatewayConfig{}
	cfg.ApplyDefaults()
	var receivedSocketPath string

	newBridgeServer(cfg, nil, nil, func(socketPath string, _ tui.HealthMonitor, _ tui.AntennaProvider) *tui.BridgeServer {
		receivedSocketPath = socketPath
		return nil
	})

	if receivedSocketPath != cfg.SocketPath {
		t.Fatalf("bridge constructor socket path = %q, want configured default %q", receivedSocketPath, cfg.SocketPath)
	}
}

func TestHealthServerAddress(t *testing.T) {
	tests := []struct {
		name       string
		listenAddr string
		port       int
		want       string
	}{
		{
			name:       "IPv4 loopback",
			listenAddr: "127.0.0.1",
			port:       8080,
			want:       "127.0.0.1:8080",
		},
		{
			name:       "IPv6 loopback",
			listenAddr: "::1",
			port:       8080,
			want:       "[::1]:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := healthServerAddress(tt.listenAddr, tt.port); got != tt.want {
				t.Fatalf("healthServerAddress(%q, %d) = %q, want %q", tt.listenAddr, tt.port, got, tt.want)
			}
		})
	}
}

func TestCacheDBPathUsesConfiguredDataPath(t *testing.T) {
	tests := []struct {
		name     string
		dataPath string
		expected string
	}{
		{
			name:     "absolute data path",
			dataPath: "/var/lib/amg-rfid-gateway",
			expected: filepath.Join("/var/lib/amg-rfid-gateway", "cache.db"),
		},
		{
			name:     "relative data path",
			dataPath: "./runtime-data",
			expected: filepath.Join("runtime-data", "cache.db"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.GatewayConfig{DataPath: tt.dataPath}

			actual := cacheDBPath(cfg)

			if actual != tt.expected {
				t.Fatalf("cacheDBPath() = %q, expected %q", actual, tt.expected)
			}
		})
	}
}

func TestCacheDBPathUsesDefaultDataPath(t *testing.T) {
	cfg := &config.GatewayConfig{}
	cfg.ApplyDefaults()

	actual := cacheDBPath(cfg)
	expected := filepath.Join("data", "cache.db")

	if actual != expected {
		t.Fatalf("cacheDBPath() = %q, expected %q", actual, expected)
	}
}

func TestLoadConfigReturnsExistingYAMLError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := `
gateway_id: "gw-1"
company_id: "company-1"
cloud_url: "wss://example.com/ws"
jwt_secret: "secret"
web_enabled: true
web_listen_addr: "0.0.0.0"
antennas: []
`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := loadConfig(configPath)
	if err == nil {
		t.Fatal("expected loadConfig to return existing YAML error")
	}

	errText := err.Error()
	for _, expected := range []string{
		"failed to load config from",
		"legacy config detected",
		`web_access_mode: "lan"`,
		"web_auth_token",
		"127.0.0.1",
	} {
		if !strings.Contains(errText, expected) {
			t.Fatalf("expected error %q to contain %q", errText, expected)
		}
	}
}
