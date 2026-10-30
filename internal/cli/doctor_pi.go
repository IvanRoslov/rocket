package cli

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

var piListTimeout = 5 * time.Second

var runPiList = func(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "pi", "list")
	cmd.WaitDelay = time.Second
	return cmd.CombinedOutput()
}

// checkPiAgent adds Pi-specific checks only when the generic availability
// check has established a usable Pi binary and version.
func checkPiAgent() []checkResult {
	a, ok := agent.Registry()["pi"]
	if !ok || a.Available() != nil {
		return nil
	}
	return []checkResult{checkPiSuperpowers()}
}

// checkPiSuperpowers verifies the native Superpowers package without
// changing Pi's global packages, settings, or credentials.
func checkPiSuperpowers() checkResult {
	const name = "pi:superpowers"
	const hint = "плагин superpowers не найден — выполните `pi install git:github.com/obra/superpowers`"
	ctx, cancel := context.WithTimeout(context.Background(), piListTimeout)
	defer cancel()
	out, err := runPiList(ctx)
	if err != nil {
		return checkResult{statusWarn, name, hint}
	}
	if !strings.Contains(string(out), "git:github.com/obra/superpowers") {
		return checkResult{statusWarn, name, hint}
	}
	return checkResult{statusOK, name, "плагин superpowers установлен"}
}
