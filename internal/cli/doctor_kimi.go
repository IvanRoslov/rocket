package cli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// kimiDoctorHome mirrors the kimi-code adapter's home lookup
// ($KIMI_CODE_HOME, default ~/.kimi-code). Doctor only reads from it.
func kimiDoctorHome() string {
	if h := os.Getenv("KIMI_CODE_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kimi-code")
}

// checkKimiAgent returns the Kimi-specific checks — only when the kimi-code
// agent is usable (its binary is the generic "agent:kimi-code" check), so a
// machine without Kimi gets no extra noise. Both are warnings.
func checkKimiAgent() []checkResult {
	a, ok := agent.Registry()["kimi-code"]
	if !ok || a.Available() != nil {
		return nil
	}
	return []checkResult{checkKimiLogin(), checkKimiSuperpowers()}
}

// checkKimiLogin: `kimi login` has run — config.toml declares the
// managed:kimi-code provider and credentials/ is non-empty.
func checkKimiLogin() checkResult {
	const name = "kimi-code:login"
	const hint = "вход не выполнен — выполните `kimi login`"
	home := kimiDoctorHome()
	if home == "" {
		return checkResult{statusWarn, name, "не удалось определить home directory"}
	}
	var cfg struct {
		Providers map[string]any `toml:"providers"`
	}
	if _, err := toml.DecodeFile(filepath.Join(home, "config.toml"), &cfg); err != nil {
		return checkResult{statusWarn, name, hint}
	}
	if _, ok := cfg.Providers["managed:kimi-code"]; !ok {
		return checkResult{statusWarn, name, hint}
	}
	entries, err := os.ReadDir(filepath.Join(home, "credentials"))
	if err != nil || len(entries) == 0 {
		return checkResult{statusWarn, name, hint}
	}
	return checkResult{statusOK, name, "вход выполнен"}
}

// checkKimiSuperpowers: the Superpowers plugin is installed and enabled in
// Kimi's plugins/installed.json (rocket prompts assume it).
func checkKimiSuperpowers() checkResult {
	const name = "kimi-code:superpowers"
	home := kimiDoctorHome()
	if home == "" {
		return checkResult{statusWarn, name, "не удалось определить home directory"}
	}
	raw, err := os.ReadFile(filepath.Join(home, "plugins", "installed.json"))
	if err != nil {
		return checkResult{statusWarn, name, "плагин superpowers не установлен в Kimi Code"}
	}
	var doc struct {
		Plugins []struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return checkResult{statusWarn, name, "не удалось прочитать plugins/installed.json"}
	}
	for _, p := range doc.Plugins {
		if p.ID != "superpowers" {
			continue
		}
		if p.Enabled {
			return checkResult{statusOK, name, "плагин superpowers включён"}
		}
		return checkResult{statusWarn, name, "плагин superpowers выключен в Kimi Code"}
	}
	return checkResult{statusWarn, name, "плагин superpowers не установлен в Kimi Code"}
}
