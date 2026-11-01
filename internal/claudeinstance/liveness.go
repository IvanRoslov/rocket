package claudeinstance

import "context"

// ExactLiveness inspects an already verified saved root without consulting a
// replacement generation or inferring process death from activity/receipts.
func ExactLiveness(ctx context.Context, id Identity, probe Probe) Liveness {
	if id.PID <= 0 || id.ProcessStart.IsZero() || id.ClaudeSessionID == "" || probe == nil {
		return Unknown
	}
	p, exists, err := probe.Process(ctx, id.PID)
	if err != nil {
		return Unknown
	}
	if !exists || p.PID != id.PID || !p.Start.Equal(id.ProcessStart) {
		return Dead
	}
	return Verified
}
