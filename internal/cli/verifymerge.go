package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// verifyMergeSession is the subset of the API session card needed to locate
// the worker's branch and repo.
type verifyMergeSession struct {
	ID     string `json:"id"`
	Branch string `json:"branch"`
	RepoID string `json:"repo_id"`
}

// verifyMergeRepo is the subset of a GET /v1/repos row needed here.
type verifyMergeRepo struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch"`
}

// gitVerifyTimeout bounds the fetch+diff sequence — a hung remote must not
// hang the CLI.
const gitVerifyTimeout = 60 * time.Second

func newVerifyMergeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-merge <subtask-id | worker-session-id>",
		Short: "Контент-проверка мержа PR подзадачи (только remote-ref'ы, не зависит от cwd)",
		Long: `Сравнивает origin/<default-branch> с origin/<веткой воркера> ТОЛЬКО по
remote-ref'ам: результат не зависит ни от текущей директории, ни от
протухшего локального чекаута, ни от незакоммиченных правок. Это
безопасная замена ручному "git diff origin/main HEAD", который молча
меняет смысл в зависимости от cwd.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket verify-merge <subtask-id | worker-session-id>"}
			}
			c, _, err := connect(false)
			if err != nil {
				return err
			}

			sessionID := args[0]
			if _, numErr := strconv.ParseInt(args[0], 10, 64); numErr == nil {
				var task taskDetailRow
				if err := c.Get(apiPath("v1", "tasks", args[0]), nil, &task); err != nil {
					return err
				}
				if task.SessionID == "" {
					return fmt.Errorf("у подзадачи #%s нет привязанной сессии — указать session id воркера напрямую", args[0])
				}
				sessionID = task.SessionID
			}

			var sess verifyMergeSession
			if err := c.Get(apiPath("v1", "sessions", sessionID), nil, &sess); err != nil {
				return err
			}
			if sess.Branch == "" {
				return fmt.Errorf("у сессии %s нет ветки", sessionID)
			}

			var repos []verifyMergeRepo
			if err := c.Get(apiPath("v1", "repos"), nil, &repos); err != nil {
				return err
			}
			var repo *verifyMergeRepo
			for i := range repos {
				if repos[i].ID == sess.RepoID {
					repo = &repos[i]
					break
				}
			}
			if repo == nil {
				return fmt.Errorf("репозиторий %q не найден в реестре", sess.RepoID)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), gitVerifyTimeout)
			defer cancel()
			res, err := runGitVerifyMerge(ctx, repo.Path, repo.DefaultBranch, sess.Branch)
			if err != nil {
				return err
			}
			cmd.Print(formatVerifyMergeReport(repo.DefaultBranch, sess.Branch, res))
			return nil
		},
	}
	return cmd
}

// gitVerifyResult carries the raw evidence runGitVerifyMerge gathered.
type gitVerifyResult struct {
	branchExists bool   // origin/<branch> still present on the remote
	treeDiff     string // git diff --stat origin/<default> origin/<branch> (whole-tree)
	ownDiff      string // git diff --stat origin/<default>...origin/<branch> (branch's own changes)
}

// runGitVerifyMerge fetches origin and collects the two diffs used by the
// report. All refs are remote-tracking — the repo's HEAD, checked-out
// branch and working tree deliberately play no part.
func runGitVerifyMerge(ctx context.Context, repoPath, defaultBranch, branch string) (gitVerifyResult, error) {
	var res gitVerifyResult

	if out, err := exec.CommandContext(ctx, "git", "-C", repoPath, "fetch", "origin", "--prune", "--quiet").CombinedOutput(); err != nil {
		return res, fmt.Errorf("git fetch origin в %s: %w (%s)", repoPath, err, strings.TrimSpace(string(out)))
	}

	baseRef := "origin/" + defaultBranch
	branchRef := "origin/" + branch

	if err := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--verify", "--quiet", branchRef).Run(); err != nil {
		return res, nil // branchExists=false: удалена на origin (обычно после сквош-мержа)
	}
	res.branchExists = true

	tree, err := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--stat", baseRef, branchRef).Output()
	if err != nil {
		return res, fmt.Errorf("git diff %s %s: %w", baseRef, branchRef, err)
	}
	res.treeDiff = strings.TrimSpace(string(tree))

	own, err := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--stat", baseRef+"..."+branchRef).Output()
	if err != nil {
		return res, fmt.Errorf("git diff %s...%s: %w", baseRef, branchRef, err)
	}
	res.ownDiff = strings.TrimSpace(string(own))

	return res, nil
}

// formatVerifyMergeReport renders the human verdict. A non-empty tree diff
// is NOT automatically a failure: after this PR merged, OTHER PRs may have
// landed in the default branch, and the whole-tree diff shows them too. The
// report therefore always pairs the tree diff with the branch's OWN files
// (three-dot diff), so the reader checks only the intersection.
func formatVerifyMergeReport(defaultBranch, branch string, res gitVerifyResult) string {
	var b strings.Builder
	baseRef := "origin/" + defaultBranch
	branchRef := "origin/" + branch

	if !res.branchExists {
		fmt.Fprintf(&b, "⚠️  %s не существует: ветка удалена на origin (обычно это происходит после сквош-мержа PR).\n", branchRef)
		fmt.Fprintf(&b, "Контент-проверка по remote-ref'ам невозможна. Проверьте вручную: PR в состоянии merged,\n")
		fmt.Fprintf(&b, "и сквош-коммит виден в git log %s с ожидаемыми файлами.\n", baseRef)
		return b.String()
	}

	if res.treeDiff == "" {
		fmt.Fprintf(&b, "✅ Деревья %s и %s идентичны — содержимое ветки полностью в %s.\n", baseRef, branchRef, defaultBranch)
		return b.String()
	}

	fmt.Fprintf(&b, "Деревья %s и %s различаются:\n\n%s\n\n", baseRef, branchRef, res.treeDiff)
	fmt.Fprintf(&b, "Это НЕ обязательно провал мержа: после этого PR в %s могли уехать другие PR —\n", defaultBranch)
	fmt.Fprintf(&b, "их файлы тоже попадают в этот диф.\n\n")
	if res.ownDiff != "" {
		fmt.Fprintf(&b, "Собственные изменения ветки (%s...%s):\n\n%s\n\n", baseRef, branchRef, res.ownDiff)
	}
	fmt.Fprintf(&b, "Проверьте пересечение: если файлы из «собственных изменений ветки» присутствуют\n")
	fmt.Fprintf(&b, "в верхнем дифе (то есть их содержимое в %s отличается от ветки) — мерж неполный,\n", defaultBranch)
	fmt.Fprintf(&b, "разбирайтесь. Если верхний диф состоит только из чужих файлов — всё в порядке.\n")
	return b.String()
}
