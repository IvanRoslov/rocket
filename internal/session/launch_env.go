package session

import "github.com/IvanRoslov/rocket/internal/agent"

func prepareLaunchEnv(a agent.Agent, spec agent.LaunchSpec, repoEnv map[string]string) (map[string]string, []string) {
	env := mergeEnv(repoEnv, a.Env(spec))
	unsetEnv := agent.LaunchEnvUnsets(a, spec)
	for _, key := range unsetEnv {
		delete(env, key)
	}
	return env, unsetEnv
}
