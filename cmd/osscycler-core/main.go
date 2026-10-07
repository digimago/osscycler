// Command osscycler-core is the headless core: it owns the ANT stick,
// tracks the trainer and heart rate monitor, and streams state to renderers
// over an authenticated gRPC API.
//
// Secrets come from the environment: ANT_PLUS_NETWORK_KEY for the radio and
// OSSCYCLER_API_TOKEN (or -token-file) for the API.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/digimago/osscycler/ant"
	"github.com/digimago/osscycler/api"
	"github.com/digimago/osscycler/control"
	"github.com/digimago/osscycler/course"
	"github.com/digimago/osscycler/erg"
	"github.com/digimago/osscycler/fec"
	"github.com/digimago/osscycler/history"
	"github.com/digimago/osscycler/home"
	"github.com/digimago/osscycler/metrics"
	"github.com/digimago/osscycler/profile"
	"github.com/digimago/osscycler/record"
	"github.com/digimago/osscycler/ride"
	"github.com/digimago/osscycler/rider"
	"github.com/digimago/osscycler/sim"
	"github.com/digimago/osscycler/telemetry"
	"github.com/digimago/osscycler/workout"
)

const keyEnv = "ANT_PLUS_NETWORK_KEY"

// Set at build time by `make release` (-ldflags -X); never in the source.
var (
	// builtinNetworkKey is the ANT+ network key compiled into release
	// binaries, so riders needn't fetch it; $ANT_PLUS_NETWORK_KEY wins.
	builtinNetworkKey string
	// releaseVersion names a release build; see version.
	releaseVersion string
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "osscycler-core:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		port          = flag.String("port", "/dev/ttyANT", "ANT stick tty (see deploy/udev)")
		addr          = flag.String("addr", api.DefaultAddr, "API listen address")
		tokenFile     = flag.String("token-file", home.Token(), "file holding the API token; created on first start ($"+api.TokenEnv+" takes precedence unless this is given)")
		newToken      = flag.Bool("new-token", false, "print a fresh random API token and exit")
		keyCheck      = flag.Bool("key-check", false, "say where the ANT+ network key comes from (never printing it) and exit")
		showVersion   = flag.Bool("version", false, "print the version and exit")
		tlsCert       = flag.String("tls-cert", "", "TLS certificate; required to listen beyond loopback")
		tlsKey        = flag.String("tls-key", "", "TLS private key")
		fake          = flag.Bool("fake", false, "serve a synthetic ride instead of using the ANT stick")
		trainerDevice = flag.Uint("trainer-device", 0, "trainer ANT device number (0 = first found)")
		hrmDevice     = flag.Uint("hrm-device", 0, "heart rate monitor ANT device number (0 = first found)")
		noHRM         = flag.Bool("no-hrm", false, "don't search for a heart rate monitor")
		debug         = flag.Bool("debug", false, "debug logging, including every raw ANT frame")
		rawPath       = flag.String("rawlog", "", "append raw ANT frames to this file")
		courseDir     = flag.String("courses", home.Courses(), "directory of .gpx courses to offer for rides")
		difficulty    = flag.Float64("difficulty", 50, "trainer difficulty in percent for this run, overriding the profile (testing). As in Zwift: climbs × difficulty, descents × half × difficulty; riding time always uses the real grade")
		maxGrade      = flag.Float64("max-grade", 16, "steepest grade the trainer can apply, percent")
		cda           = flag.Float64("cda", 0.32, "drag area for the ride simulation, m² (0.32 hoods, 0.25 drops)")
		crr           = flag.Float64("crr", 0.004, "rolling resistance for the ride simulation")
		rideStart     = flag.Float64("ride-start-m", 0, "start course rides this many metres in, rolling (practise a section; the demo uses it)")
		workoutDir    = flag.String("workouts", home.Workouts(), "directory of .zwo workouts")
		ftp           = flag.Float64("ftp", 0, "FTP in watts for this run, overriding the profile (testing)")
		recordDir     = flag.String("record", home.Rides(), "record every ride as a FIT file in this directory (created if needed), with course results in "+record.ResultsFile)
		noRecord      = flag.Bool("no-record", false, "don't record rides")
		profilePath   = flag.String("profile", home.Profile(), "rider profile (weight, FTP, difficulty); created by onboarding. -rider-kg, -ftp and -difficulty override it for one run without saving")
		recordGPS     = flag.Bool("record-gps", true, "put the course's map position (from its GPX) in recorded course rides; uploads then show the route on a map")
		user          fec.UserConfig
	)
	flag.Float64Var(&user.UserWeightKg, "rider-kg", 0, "rider weight for this run, overriding the profile (testing)")
	flag.Float64Var(&user.BikeWeightKg, "bike-kg", 9, "bike weight for user config")
	flag.Float64Var(&user.WheelDiameterM, "wheel-m", 0.672, "wheel diameter for user config")
	flag.Parse()

	if *newToken {
		fmt.Println(api.NewToken())
		return nil
	}
	if *showVersion {
		fmt.Println(version())
		return nil
	}
	if *keyCheck {
		_, source, err := loadNetworkKey()
		if err != nil {
			return err
		}
		fmt.Println("ANT+ network key: valid,", source)
		return nil
	}
	if *trainerDevice > 0xFFFF || *hrmDevice > 0xFFFF {
		return errors.New("device numbers must fit in 16 bits")
	}
	if *difficulty < 0 || *difficulty > 100 {
		return errors.New("-difficulty must be between 0 and 100")
	}

	// The rider's settings come from the profile; flags given explicitly
	// override them for this run only (testing and validation).
	prof, err := profile.Open(*profilePath)
	if err != nil {
		return fmt.Errorf("%w (fix or remove the file, or pass -profile)", err)
	}
	bikeSet := false
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "rider-kg":
			prof.Force(profile.Weight, user.UserWeightKg)
		case "ftp":
			prof.Force(profile.FTP, *ftp)
		case "difficulty":
			prof.Force(profile.Difficulty, *difficulty)
		case "bike-kg":
			bikeSet = true
		}
	})
	rp := prof.Get()
	user.UserWeightKg = rp.WeightKg
	if !bikeSet {
		user.BikeWeightKg = rp.BikeKg
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	// osscycler's folder (~/osscycler): made on first start when any of
	// it is used, with data moved over from where 0.1.0 kept it.
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if !set["courses"] || !set["workouts"] || !set["record"] || !set["profile"] || !set["token-file"] {
		moved, err := home.Ensure()
		if err != nil {
			return err
		}
		for _, m := range moved {
			fmt.Fprintln(os.Stderr, "osscycler-core: moved", m)
		}
	}

	// The token: an explicit -token-file, else $OSSCYCLER_API_TOKEN, else
	// the one in osscycler's folder, made on first start.
	var token string
	switch {
	case set["token-file"]:
		token, err = api.LoadToken(*tokenFile)
	case os.Getenv(api.TokenEnv) != "":
		token, err = api.LoadToken("")
	default:
		var created bool
		token, created, err = home.EnsureToken()
		if created {
			fmt.Fprintln(os.Stderr, "osscycler-core: created the API token in", home.Token())
		}
	}
	if err != nil {
		return err
	}
	var serverOpts []grpc.ServerOption
	switch {
	case *tlsCert != "" || *tlsKey != "":
		cert, err := tls.LoadX509KeyPair(*tlsCert, *tlsKey)
		if err != nil {
			return err
		}
		serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
		})))
	case !api.IsLoopback(*addr):
		return fmt.Errorf("refusing to serve %s without TLS: the API can change trainer resistance; pass -tls-cert and -tls-key", *addr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The source of telemetry: the ANT stick, or a synthetic ride.
	hub := telemetry.NewHub()
	var source interface {
		telemetry.Calibrator
		ride.Trainer
		erg.Trainer
		rider.UserSetter
		control.Trainer
		Run(context.Context) error
	}
	var stats metrics.StatsSource // the stick's counters; none when faking
	if *fake {
		log.Info("serving a synthetic ride")
		source = telemetry.NewFake(hub)
	} else {
		cfg := telemetry.Config{
			TrainerDevice: uint16(*trainerDevice),
			HRMDevice:     uint16(*hrmDevice),
			DisableHRM:    *noHRM,
			User:          user,
		}
		if cfg.NetworkKey, _, err = loadNetworkKey(); err != nil {
			return err
		}
		// The core runs without the stick and picks it up when it appears,
		// also after it's unplugged.
		stick := telemetry.NewStick(func() (*ant.Node, func(), error) {
			return openStick(*port, *debug, *rawPath, log)
		}, hub, cfg, log)
		source = stick
		stats = stick
	}

	// History (results, PBs, ghosts) reads the recording directory, also
	// with -no-record: earlier rides still count.
	var hist *history.Store
	var ghosts ride.GhostSource
	if *recordDir != "" {
		hist = &history.Store{Dir: *recordDir}
		ghosts = hist
	}
	rides := newRides(hub, source, *courseDir, user, rp.DifficultyPct, *maxGrade, *cda, *crr, *rideStart, ghosts, log)

	var workouts *erg.Session
	if *workoutDir != "" {
		ftpW := rp.FTPW
		if ftpW == 0 {
			ftpW = 200 // a placeholder: workouts wait for the rider's FTP
		}
		workouts = erg.NewSession(hub, source, &workout.Library{Dir: *workoutDir}, erg.DefaultConfig(ftpW), log)
	}

	// The rider service applies profile changes everywhere, saves them, and
	// holds rides and workouts back until the profile supports them.
	riders := rider.New(prof, hub, source, rides, workouts, user, log)
	manual := control.New(hub, source, *maxGrade, log)
	svc := api.Services{Calibrator: source, Rides: riders.Rides(), Profile: riders, Control: manual}
	if workouts != nil {
		svc.Workouts = riders.Workouts()
	}
	if hist != nil {
		svc.History = hist
		svc.Activities = &record.Catalog{Dir: *recordDir}
	}
	var rec *record.Recorder
	if !*noRecord && *recordDir != "" {
		cfg := record.DefaultConfig(*recordDir)
		cfg.GPS = *recordGPS
		rec = record.New(hub, cfg, log)
		svc.Recorder = rec
	}
	srv, err := api.NewServer(hub, svc, token, serverOpts...)
	if err != nil {
		return err
	}
	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()
	defer srv.Stop()
	log.Info("API listening", "addr", lis.Addr().String(), "tls", len(serverOpts) > 0)

	stopMetrics, err := metrics.Start(ctx, metrics.Sources{Hub: hub, ANT: stats, Channels: telemetry.ChannelNames}, version(), log)
	if err != nil {
		log.Warn("metrics off", "err", err) // never a reason not to ride
	}
	defer stopMetrics()

	done := make(chan error, 1)
	go func() { done <- source.Run(ctx) }()
	go telemetry.LogEvents(ctx, hub, log)
	go rides.Run(ctx)
	go manual.Run(ctx)
	if workouts != nil {
		go workouts.Run(ctx)
	}
	// The recorder saves the ride under way when ctx ends; wait for it.
	recorded := make(chan struct{})
	if rec != nil {
		log.Info("recording rides", "dir", *recordDir)
		go func() {
			defer close(recorded)
			if err := rec.Run(ctx); err != nil {
				log.Error("recorder stopped", "err", err)
			}
		}()
	} else {
		log.Warn("rides are not recorded")
		close(recorded)
	}

	select {
	case err = <-done:
		stop()
	case err = <-serveErr:
		stop()
		<-done
		err = fmt.Errorf("API server: %w", err)
	}
	<-recorded
	return err
}

// version is the release's, else the module version, or the VCS
// revision of a local build.
func version() string {
	if releaseVersion != "" {
		return releaseVersion
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	v := bi.Main.Version
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 && (v == "" || v == "(devel)") {
			v = s.Value[:12]
		}
	}
	return v
}

// newRides loads the courses and sets up the ride simulation.
func newRides(hub *telemetry.Hub, trainer ride.Trainer, dir string, user fec.UserConfig,
	difficulty, maxGrade, cda, crr, startM float64, ghosts ride.GhostSource, log *slog.Logger) *ride.Session {
	var courses []*course.Course
	if dir != "" {
		var err error
		courses, err = course.LoadDir(dir)
		if err != nil {
			log.Warn("some courses failed to load", "err", err)
		}
		if len(courses) == 0 && err == nil {
			log.Warn("no courses yet: put .gpx files in the courses folder and restart", "dir", dir)
		}
		for _, c := range courses {
			log.Info("course", "id", c.ID, "name", c.Name, "distance_km", fmt.Sprintf("%.2f", c.Distance/1000),
				"gain_m", fmt.Sprintf("%.0f", c.Gain), "max_grade", fmt.Sprintf("%.1f", c.MaxGrade))
		}
	}
	riderKg := user.UserWeightKg
	if riderKg == 0 {
		riderKg = 75 // a placeholder: course rides wait for the rider's weight
	}
	params := sim.DefaultParams(riderKg, user.BikeWeightKg)
	params.CdA, params.Crr = cda, crr
	cfg := ride.DefaultConfig(params)
	cfg.Difficulty, cfg.MaxGradePct, cfg.StartDistanceM = difficulty/100, maxGrade, startM
	cfg.Ghosts = ghosts
	return ride.NewSession(hub, trainer, courses, cfg, log)
}

// loadNetworkKey takes the key from the environment, or else the one
// built into a release binary. source says which, for -key-check.
func loadNetworkKey() (k ant.NetworkKey, source string, err error) {
	if s := os.Getenv(keyEnv); s != "" {
		k, err := ant.ParseNetworkKey(s)
		if err != nil {
			return k, "", fmt.Errorf("%s: %w", keyEnv, err)
		}
		return k, "$" + keyEnv, nil
	}
	if builtinNetworkKey != "" {
		k, err := ant.ParseNetworkKey(builtinNetworkKey)
		if err != nil {
			return k, "", fmt.Errorf("the network key built into this binary is invalid (a broken release build): %w", err)
		}
		return k, "built in", nil
	}
	return k, "", fmt.Errorf("no ANT+ network key: set %s (or use -fake). Accept the ANT+ Adopter Agreement at "+
		"https://developer.garmin.com/ant-program/downloads/ and copy the ANT+ key from "+
		"https://developer.garmin.com/ant-program/ant-ant-plus/network-keys/", keyEnv)
}

// openStick opens the ANT stick with optional frame tracing and returns a
// function that closes it and flushes the raw log.
func openStick(port string, debug bool, rawPath string, log *slog.Logger) (*ant.Node, func(), error) {
	var (
		fl   *ant.FrameLog
		file *os.File
	)
	if rawPath != "" {
		f, err := os.OpenFile(rawPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, err
		}
		file, fl = f, ant.NewFrameLog(f)
	}
	closeLog := func() {
		if fl != nil {
			fl.Flush()
			file.Close()
		}
	}
	rw, err := ant.OpenSerial(port)
	if err != nil {
		closeLog()
		if errors.Is(err, fs.ErrNotExist) {
			err = fmt.Errorf("%w (is the ANT+ stick plugged in? its udev rule installed? or pass -port)", err)
		}
		return nil, nil, err
	}
	node := ant.NewNode(rw, ant.Options{Logger: log, Trace: func(dir ant.Direction, frame []byte) {
		if debug {
			log.Debug("frame", "dir", dir, "bytes", fmt.Sprintf("% X", frame))
		}
		if fl != nil {
			fl.Trace(dir, frame)
		}
	}})
	return node, func() { node.Close(); closeLog() }, nil
}
