package telemetry

import (
	"context"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/digimago/osscycler/internal/ant"
	"github.com/digimago/osscycler/internal/fec"
)

// DefaultStickPoll is how often a missing ANT+ stick is looked for.
const DefaultStickPoll = 2 * time.Second

// Opener opens the ANT+ stick: the node, and a func to close it.
type Opener func() (*ant.Node, func(), error)

// Stick keeps a Service running on the ANT+ stick: it opens the stick
// when it appears and again whenever it's unplugged and back, so the core
// stays up without it. Commands while there is no stick answer
// ErrTrainerUnavailable. Radio in the state says whether it's there.
type Stick struct {
	open Opener
	hub  *Hub
	log  *slog.Logger
	Poll time.Duration

	mu   sync.Mutex
	cfg  Config // its User follows SetUser across reopenings
	svc  *Service
	node *ant.Node
	base ant.Stats // counters of earlier openings, so totals never go back
}

func NewStick(open Opener, hub *Hub, cfg Config, log *slog.Logger) *Stick {
	return &Stick{open: open, hub: hub, cfg: cfg, log: log, Poll: DefaultStickPoll}
}

// Run looks for the stick, runs the service on it, and starts over when it
// goes, until ctx is done.
func (s *Stick) Run(ctx context.Context) error {
	lastErr := ""
	for {
		node, closeNode, err := s.open()
		if err != nil {
			if err.Error() != lastErr {
				s.log.Warn("ANT+ stick not available; looking for it", "err", err, "every", s.Poll)
				lastErr = err.Error()
			}
			s.publish(false, err.Error())
		} else {
			lastErr = ""
			s.mu.Lock()
			svc := NewService(node, s.hub, s.cfg, s.log)
			s.svc, s.node = svc, node
			s.mu.Unlock()
			s.publish(true, "")
			s.log.Info("ANT+ stick ready")

			runErr := svc.Run(ctx)
			closeNode()
			s.mu.Lock()
			s.base = addStats(s.base, node.Stats())
			s.cfg.User = svc.user()
			s.svc, s.node = nil, nil
			s.mu.Unlock()
			s.hub.Update(func(st *State) bool {
				a := markLost(st, &st.Trainer.Sensor)
				b := markLost(st, &st.HeartRate.Sensor)
				return a || b
			})
			if ctx.Err() != nil {
				return nil
			}
			msg := "stopped"
			if runErr != nil {
				msg = runErr.Error()
			}
			s.log.Warn("ANT+ stick gone; looking for it", "err", msg)
			s.publish(false, msg)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.Poll):
		}
	}
}

func (s *Stick) publish(present bool, why string) {
	s.hub.Update(func(st *State) bool {
		r := Radio{Known: true, Present: present, Error: why}
		if st.Radio == r {
			return false
		}
		st.Radio = r
		return true
	})
}

func (s *Stick) service() *Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.svc
}

func (s *Stick) StartSpinDown(ctx context.Context) error {
	if svc := s.service(); svc != nil {
		return svc.StartSpinDown(ctx)
	}
	return ErrTrainerUnavailable
}

func (s *Stick) CancelCalibration(ctx context.Context) error {
	if svc := s.service(); svc != nil {
		return svc.CancelCalibration(ctx)
	}
	return ErrTrainerUnavailable
}

func (s *Stick) SetGrade(ctx context.Context, gradePct float64) error {
	if svc := s.service(); svc != nil {
		return svc.SetGrade(ctx, gradePct)
	}
	return ErrTrainerUnavailable
}

func (s *Stick) SetTargetPower(ctx context.Context, watts float64) error {
	if svc := s.service(); svc != nil {
		return svc.SetTargetPower(ctx, watts)
	}
	return ErrTrainerUnavailable
}

func (s *Stick) SetResistance(ctx context.Context, pct float64) error {
	if svc := s.service(); svc != nil {
		return svc.SetResistance(ctx, pct)
	}
	return ErrTrainerUnavailable
}

// SetUser keeps the user config for every opening of the stick, and sends
// it now if the trainer is there.
func (s *Stick) SetUser(ctx context.Context, u fec.UserConfig) error {
	s.mu.Lock()
	s.cfg.User = u
	svc := s.svc
	s.mu.Unlock()
	if svc != nil {
		return svc.SetUser(ctx, u)
	}
	return nil
}

// Stats are the transport counters over every opening of the stick.
func (s *Stick) Stats() ant.Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.node == nil {
		return addStats(s.base, ant.Stats{})
	}
	return addStats(s.base, s.node.Stats())
}

func addStats(a, b ant.Stats) ant.Stats {
	out := ant.Stats{
		Rx: a.Rx + b.Rx, Tx: a.Tx + b.Tx, DroppedBytes: a.DroppedBytes + b.DroppedBytes,
		BadChecksums: a.BadChecksums + b.BadChecksums, Overflows: a.Overflows + b.Overflows,
		Channels: map[byte]ant.ChannelStats{},
	}
	for _, m := range []map[byte]ant.ChannelStats{a.Channels, b.Channels} {
		for ch, cs := range m {
			sum := out.Channels[ch]
			sum.Data += cs.Data
			if sum.RFEvents == nil {
				sum.RFEvents = map[ant.EventCode]uint64{}
			} else {
				sum.RFEvents = maps.Clone(sum.RFEvents)
			}
			for code, n := range cs.RFEvents {
				sum.RFEvents[code] += n
			}
			out.Channels[ch] = sum
		}
	}
	return out
}
