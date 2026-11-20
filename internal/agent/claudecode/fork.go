package claudecode

import (
	"context"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

func (a *Adapter) Fork(ctx context.Context, s agent.SessionRef, r agent.ForkReq) (agent.SessionRef, error) {
	return agent.SessionRef{}, agent.ErrUnsupported
}

func (a *Adapter) Events(ctx context.Context, s agent.SessionRef, since string) (<-chan agent.Event, error) {
	return nil, agent.ErrUnsupported
}
