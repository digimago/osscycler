package telemetry

import (
	"context"
	"log/slog"

	"github.com/digimago/osscycler/internal/fec"
)

// LogEvents logs calibration steps and trainer status changes until ctx is
// done, so the core's log records what happened even when no renderer was
// watching.
func LogEvents(ctx context.Context, hub *Hub, log *slog.Logger) {
	var (
		lastCal    Calibration
		lastFlags  fec.TrainerStatus
		flagsKnown bool
	)
	for {
		st, changed := hub.Latest()

		c := st.Trainer.Calibration
		if c.Phase != lastCal.Phase || c.SpeedCondition != lastCal.SpeedCondition || c.Message != lastCal.Message {
			log.Info("calibration", calibrationAttrs(c)...)
			lastCal = c
		}

		// Flags only mean something once the trainer is connected.
		if f := st.Trainer.Flags; st.Trainer.Sensor.Status == StatusConnected && (!flagsKnown || f != lastFlags) {
			log.Info("trainer status", "flags", f)
			lastFlags, flagsKnown = f, true
		}

		select {
		case <-ctx.Done():
			return
		case <-changed:
		}
	}
}

func calibrationAttrs(c Calibration) []any {
	attrs := []any{"phase", c.Phase}
	if c.Phase == CalInProgress {
		attrs = append(attrs, "speed", c.SpeedCondition)
		if c.TargetSpeedMPS.OK {
			attrs = append(attrs, "target_kmh", int(c.TargetSpeedMPS.V*3.6+0.5))
		}
	}
	if c.TemperatureC.OK {
		attrs = append(attrs, "temperature_c", c.TemperatureC.V)
	}
	if c.SpinDownMS.OK {
		attrs = append(attrs, "spin_down_ms", c.SpinDownMS.V)
	}
	if c.TargetSpinDownMS.OK {
		attrs = append(attrs, "target_spin_down_ms", c.TargetSpinDownMS.V)
	}
	if c.Message != "" {
		attrs = append(attrs, "message", c.Message)
	}
	return attrs
}
