package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// The ACTIVITIES tab lists the recordings on the core. s saves the
// selected one as a FIT file on this machine (the core may be a Pi in the
// corner), ready to upload anywhere by hand.

// exportTimeout allows for a long ride over a slow link.
const exportTimeout = 2 * time.Minute

type activitiesMsg struct {
	activities []*pb.Activity
	err        error
}

type exportedMsg struct {
	path    string
	already bool
	err     error
}

// WithExportDir sets where exported rides are saved.
func (m Model) WithExportDir(dir string) Model {
	m.exportDir = dir
	return m
}

func (m Model) fetchActivities() tea.Cmd {
	cmds := m.cmds
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		as, err := cmds.ListActivities(ctx)
		return activitiesMsg{as, err}
	}
}

func (m Model) onActivities(msg activitiesMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.notice = "loading activities failed: " + friendlyErr(msg.err)
		return m, nil
	}
	m.activities = msg.activities
	m.pickIdx[tabActivities] = min(m.pickIdx[tabActivities], max(0, len(m.activities)-1))
	return m, nil
}

// exportSelected saves the selected recording into the export directory.
// It downloads to a temporary file and renames it when complete, so a
// broken connection never leaves a half file that looks finished, and it
// never overwrites a file of the same name.
func (m Model) exportSelected() tea.Cmd {
	i := m.pickIdx[tabActivities]
	if i >= len(m.activities) || m.activities[i].GetError() != "" {
		return nil
	}
	name, dir, cmds := m.activities[i].GetName(), m.exportDir, m.cmds
	return func() tea.Msg {
		if dir == "" {
			dir = "."
		}
		dest := filepath.Join(dir, name)
		if _, err := os.Stat(dest); err == nil {
			return exportedMsg{path: dest, already: true}
		}
		tmp, err := os.CreateTemp(dir, ".osscycler-*.fit.part")
		if err != nil {
			return exportedMsg{err: err}
		}
		defer os.Remove(tmp.Name()) // nothing left after the rename
		ctx, cancel := context.WithTimeout(context.Background(), exportTimeout)
		defer cancel()
		_, err = cmds.ExportActivity(ctx, name, tmp)
		err = errors.Join(err, tmp.Close())
		if err == nil {
			err = os.Rename(tmp.Name(), dest)
		}
		return exportedMsg{path: dest, err: err}
	}
}

func (m Model) onExported(msg exportedMsg) (Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		m.notice = "export failed: " + friendlyErr(msg.err)
	case msg.already:
		m.notice = "already saved: " + msg.path
	default:
		m.notice = "saved " + msg.path
	}
	return m, nil
}

func (m Model) activityRows(visible int) []string {
	if len(m.activities) == 0 {
		return []string{dimStyle.Render("  no recorded rides yet: they appear here once saved")}
	}
	sel := m.pickIdx[tabActivities]
	first := max(0, min(len(m.activities)-visible, sel-visible/2))
	var rows []string
	for i := first; i < min(len(m.activities), first+max(visible, 1)); i++ {
		a := m.activities[i]
		cursor, style := "  ", lipgloss.NewStyle()
		if i == sel {
			cursor, style = "▸ ", style.Bold(true).Foreground(lipgloss.Color("220"))
		}
		if a.GetError() != "" {
			rows = append(rows, style.Render(cursor)+badStyle.Render(fmt.Sprintf("%-22s ✕ %s", a.GetName(), truncate(a.GetError(), 50))))
			continue
		}
		kind := "indoor"
		if a.GetVirtual() {
			kind = "virtual"
		}
		power := "    -"
		if a.GetAvgPowerW() > 0 {
			power = fmt.Sprintf("%3.0f W", a.GetAvgPowerW())
		}
		start := time.UnixMilli(a.GetStartUnixMs()).Local().Format("2006-01-02 15:04")
		rows = append(rows, style.Render(fmt.Sprintf("%s%s  %8s  %6.2f km  %s  %-7s  %d lap%s",
			cursor, start, clock(a.GetTimerS()), a.GetDistanceM()/1000, power, kind, a.GetLaps(), plural(a.GetLaps()))))
	}
	return rows
}

func plural(n uint32) string {
	if n == 1 {
		return ""
	}
	return "s"
}
