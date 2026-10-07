package api

import (
	"github.com/digimago/osscycler/course"
	"github.com/digimago/osscycler/fec"
	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/scenery"
	"github.com/digimago/osscycler/telemetry"
)

// ToProto converts a telemetry snapshot to its wire form.
func ToProto(s telemetry.State) *pb.State {
	t := s.Trainer
	return &pb.State{
		Sequence:   s.Seq,
		CoreTimeNs: int64(s.Time),
		Trainer: &pb.Trainer{
			Sensor:                        sensorToProto(t.Sensor),
			State:                         trainerStateToProto(t.State),
			PowerW:                        opt(t.PowerW),
			CadenceRpm:                    opt(t.CadenceRPM),
			SpeedMps:                      optFloat(t.SpeedMPS),
			HeartRateBpm:                  opt(t.HeartRateBPM),
			DistanceM:                     t.DistanceM,
			ElapsedS:                      t.Elapsed.Seconds(),
			PowerCalibrationRequired:      t.Flags&fec.PowerCalibrationRequired != 0,
			ResistanceCalibrationRequired: t.Flags&fec.ResistanceCalibrationRequired != 0,
			UserConfigRequired:            t.Flags&fec.UserConfigRequired != 0,
			TargetPowerLimit:              targetPowerLimitToProto(t.TargetPowerLimit),
			Calibration:                   calibrationToProto(t.Calibration),
		},
		HeartRate: &pb.HeartRate{
			Sensor: sensorToProto(s.HeartRate.Sensor),
			Bpm:    opt(s.HeartRate.BPM),
		},
		Ride:    rideToProto(s.Ride),
		Workout: workoutProgressToProto(s.Workout),
		Profile: profileToProto(s.Profile),
		Radio:   radioToProto(s.Radio),
		Control: &pb.TrainerControl{
			Mode:   pb.ControlMode(s.Control.Mode) + 1, // proto reserves 0 for unspecified
			Target: s.Control.Target, ChangedNs: int64(s.Control.Changed),
		},
		Recording: &pb.Recording{
			Active: s.Recording.Active, Paused: s.Recording.Paused, File: s.Recording.File,
			TimerS: s.Recording.Timer.Seconds(), DistanceM: s.Recording.DistanceM,
			LastSaved: s.Recording.LastSaved, Error: s.Recording.Error,
		},
	}
}

func rideToProto(r telemetry.Ride) *pb.Ride {
	return &pb.Ride{
		Phase:           pb.RidePhase(r.Phase) + 1, // proto reserves 0 for unspecified
		CourseId:        r.CourseID,
		CourseName:      r.CourseName,
		CourseDistanceM: r.CourseDistanceM,
		CourseGainM:     r.CourseGainM,
		DistanceM:       r.DistanceM,
		ElevationM:      r.ElevationM,
		GradePct:        r.GradePct,
		TrainerGradePct: r.TrainerGradePct,
		SpeedMps:        r.SpeedMPS,
		ClimbedM:        r.ClimbedM,
		AvgPowerW:       r.AvgPowerW,
		ElapsedS:        r.Elapsed.Seconds(),
		PhaseChangedNs:  int64(r.Changed),
		DifficultyPct:   r.DifficultyPct,
		StartDistanceM:  r.StartDistanceM,
		LatitudeDeg:     r.Lat,
		LongitudeDeg:    r.Lon,
		Ghost:           ghostToProto(r.Ghost),
	}
}

// profileToProto converts the rider profile; nil for a core without one.
func profileToProto(p telemetry.Profile) *pb.RiderProfile {
	if !p.Known {
		return nil
	}
	out := &pb.RiderProfile{
		Complete: p.Complete, WeightKg: p.WeightKg, FtpW: p.FTPW, DifficultyPct: p.DifficultyPct,
		WeightForced: p.WeightForced, FtpForced: p.FTPForced, DifficultyForced: p.DifficultyForced,
		Path: p.Path, SuggestedFtpW: p.SuggestedFTPW,
		HeightCm: p.HeightCm, Cda: p.CdA, HeightForced: p.HeightForced, CdaForced: p.CdAForced,
	}
	if p.NeedWeight {
		out.Missing = append(out.Missing, "weight_kg")
	}
	if p.NeedFTP {
		out.Missing = append(out.Missing, "ftp_w")
	}
	return out
}

func radioToProto(r telemetry.Radio) *pb.Radio {
	if !r.Known {
		return nil
	}
	return &pb.Radio{Present: r.Present, Error: r.Error}
}

func ghostToProto(g telemetry.RideGhost) *pb.RideGhost {
	if g.Label == "" {
		return nil
	}
	return &pb.RideGhost{Label: g.Label, DistanceM: g.DistanceM, GapS: g.Gap.Seconds(), TimeS: g.Elapsed.Seconds()}
}

// CourseToProto converts a course with its full profile and track, and
// its scenery when known (sc may be nil).
func CourseToProto(c *course.Course, sc *scenery.Scenery) *pb.Course {
	ele, grade := c.Profile()
	east, north := c.Track()
	pc := &pb.Course{
		Id:                c.ID,
		Name:              c.Name,
		DistanceM:         c.Distance,
		GainM:             c.Gain,
		LossM:             c.Loss,
		MaxGradePct:       c.MaxGrade,
		MinGradePct:       c.MinGrade,
		ProfileStepM:      c.Spacing,
		ProfileElevationM: make([]float32, len(ele)),
		ProfileGradePct:   make([]float32, len(grade)),
		ProfileEastM:      make([]float32, len(east)),
		ProfileNorthM:     make([]float32, len(north)),
	}
	for i := range ele {
		pc.ProfileElevationM[i], pc.ProfileGradePct[i] = float32(ele[i]), float32(grade[i])
		pc.ProfileEastM[i], pc.ProfileNorthM[i] = float32(east[i]), float32(north[i])
	}
	if sc != nil {
		pc.Attribution = scenery.Attribution
		pc.LandUse = make([]byte, len(sc.Land))
		for i, l := range sc.Land {
			pc.LandUse[i] = byte(l)
		}
		for _, b := range sc.Buildings {
			pc.Buildings = append(pc.Buildings, &pb.Building{
				DistanceM: b.DistanceM, OffsetM: b.OffsetM, LengthM: b.LengthM, DepthM: b.DepthM,
				HeightM: b.HeightM, Kind: pb.BuildingKind(b.Kind),
			})
		}
	}
	return pc
}

func sensorToProto(s telemetry.Sensor) *pb.Sensor {
	return &pb.Sensor{
		Status:         sensorStatusToProto(s.Status),
		DeviceNumber:   uint32(s.DeviceNumber),
		ManufacturerId: uint32(s.ManufacturerID),
		ModelNumber:    uint32(s.ModelNumber),
		SwVersion:      s.SWVersion,
		LastSeenNs:     int64(s.LastSeen),
	}
}

func sensorStatusToProto(s telemetry.SensorStatus) pb.SensorStatus {
	switch s {
	case telemetry.StatusDisabled:
		return pb.SensorStatus_SENSOR_STATUS_DISABLED
	case telemetry.StatusSearching:
		return pb.SensorStatus_SENSOR_STATUS_SEARCHING
	case telemetry.StatusConnected:
		return pb.SensorStatus_SENSOR_STATUS_CONNECTED
	case telemetry.StatusLost:
		return pb.SensorStatus_SENSOR_STATUS_LOST
	}
	return pb.SensorStatus_SENSOR_STATUS_UNSPECIFIED
}

func trainerStateToProto(s fec.State) pb.TrainerState {
	switch s {
	case fec.StateAsleep:
		return pb.TrainerState_TRAINER_STATE_ASLEEP
	case fec.StateReady:
		return pb.TrainerState_TRAINER_STATE_READY
	case fec.StateInUse:
		return pb.TrainerState_TRAINER_STATE_IN_USE
	case fec.StateFinished:
		return pb.TrainerState_TRAINER_STATE_FINISHED
	}
	return pb.TrainerState_TRAINER_STATE_UNSPECIFIED
}

func calibrationToProto(c telemetry.Calibration) *pb.Calibration {
	return &pb.Calibration{
		Phase:                pb.CalibrationPhase(c.Phase) + 1, // proto reserves 0 for unspecified
		SpeedCondition:       pb.CalibrationCondition(c.SpeedCondition),
		TemperatureCondition: pb.CalibrationCondition(c.TemperatureCondition),
		TargetSpeedMps:       optFloat(c.TargetSpeedMPS),
		TemperatureC:         optFloat(c.TemperatureC),
		TargetSpinDownMs:     opt(c.TargetSpinDownMS),
		SpinDownMs:           opt(c.SpinDownMS),
		Message:              c.Message,
		PhaseChangedNs:       int64(c.Changed),
	}
}

func targetPowerLimitToProto(l fec.TargetPowerLimit) pb.TargetPowerLimit {
	switch l {
	case fec.AtTargetPower:
		return pb.TargetPowerLimit_TARGET_POWER_LIMIT_AT_TARGET
	case fec.SpeedTooLow:
		return pb.TargetPowerLimit_TARGET_POWER_LIMIT_SPEED_TOO_LOW
	case fec.SpeedTooHigh:
		return pb.TargetPowerLimit_TARGET_POWER_LIMIT_SPEED_TOO_HIGH
	case fec.LimitUndetermined:
		return pb.TargetPowerLimit_TARGET_POWER_LIMIT_UNDETERMINED
	}
	return pb.TargetPowerLimit_TARGET_POWER_LIMIT_UNSPECIFIED
}

func opt[T uint8 | uint16](o telemetry.Opt[T]) *uint32 {
	if !o.OK {
		return nil
	}
	v := uint32(o.V)
	return &v
}

func optFloat(o telemetry.Opt[float64]) *float64 {
	if !o.OK {
		return nil
	}
	return &o.V
}
