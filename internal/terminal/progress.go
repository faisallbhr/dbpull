package terminal

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	syncpkg "github.com/faisallbhr/dbpull/internal/sync"

	"golang.org/x/term"
)

const (
	ttyRenderInterval    = 500 * time.Millisecond
	nonTTYRenderInterval = 10 * time.Second
	speedEMAAlpha        = 0.2
	rateEMAAlpha         = 0.2
)

type SyncPhase int

const (
	PhaseSSH SyncPhase = iota
	PhaseSource
	PhaseTarget
	PhasePlan
	PhaseSchema
	PhaseData
)

type phaseStatus int

const (
	phasePending phaseStatus = iota
	phaseActive
	phaseDone
)

type ticker interface {
	Chan() <-chan time.Time
	Stop()
}

type realTicker struct {
	*time.Ticker
}

func (t realTicker) Chan() <-chan time.Time {
	return t.C
}

type SyncProgressRenderer struct {
	out       io.Writer
	verbose   bool
	isTTY     bool
	now       func() time.Time
	newTicker func(time.Duration) ticker

	commands chan rendererCommand
	done     chan struct{}
	started  bool
}

type rendererCommand struct {
	kind   commandKind
	phase  SyncPhase
	plan   *syncpkg.SyncPlan
	update *syncpkg.DataProgress
	err    error
	reply  chan error
}

type commandKind int

const (
	commandStart commandKind = iota
	commandStartPhase
	commandCompletePhase
	commandSetPlan
	commandHandleProgress
	commandFailure
	commandCancel
	commandClose
)

type rendererState struct {
	phases          [6]phaseStatus
	currentPhase    SyncPhase
	totalTables     int
	completedTables int
	copiedRows      int64
	startedAt       time.Time

	lastRowSampleAt time.Time
	successfulBatch int
	smoothedSpeed   float64
	smoothedETA     time.Duration
	smoothedRate    float64

	lines      int
	lastRender time.Time
}

func NewSyncProgressRenderer(out io.Writer, verbose bool) *SyncProgressRenderer {
	return &SyncProgressRenderer{
		out:     out,
		verbose: verbose,
		isTTY:   IsTTY(out),
		now:     time.Now,
		newTicker: func(interval time.Duration) ticker {
			return realTicker{Ticker: time.NewTicker(interval)}
		},
	}
}

func IsTTY(w io.Writer) bool {
	file, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return false
	}

	return term.IsTerminal(int(file.Fd()))
}

func (r *SyncProgressRenderer) Start() error {
	if r.started {
		return nil
	}

	r.commands = make(chan rendererCommand)
	r.done = make(chan struct{})
	r.started = true

	go r.run()

	return r.send(rendererCommand{kind: commandStart})
}

func (r *SyncProgressRenderer) StartPhase(phase SyncPhase) error {
	return r.send(rendererCommand{kind: commandStartPhase, phase: phase})
}

func (r *SyncProgressRenderer) CompletePhase(phase SyncPhase) error {
	return r.send(rendererCommand{kind: commandCompletePhase, phase: phase})
}

func (r *SyncProgressRenderer) SetPlan(plan syncpkg.SyncPlan) error {
	return r.send(rendererCommand{kind: commandSetPlan, plan: &plan})
}

func (r *SyncProgressRenderer) Handle(update syncpkg.DataProgress) error {
	return r.send(rendererCommand{kind: commandHandleProgress, update: &update})
}

func (r *SyncProgressRenderer) Failure(err error) error {
	return r.stop(rendererCommand{kind: commandFailure, err: err})
}

func (r *SyncProgressRenderer) Cancel(err error) error {
	return r.stop(rendererCommand{kind: commandCancel, err: err})
}

func (r *SyncProgressRenderer) Close() error {
	if !r.started {
		return nil
	}
	return r.stop(rendererCommand{kind: commandClose})
}

func (r *SyncProgressRenderer) send(command rendererCommand) error {
	if !r.started {
		return fmt.Errorf("sync progress renderer not started")
	}

	command.reply = make(chan error, 1)
	r.commands <- command
	return <-command.reply
}

func (r *SyncProgressRenderer) stop(command rendererCommand) error {
	err := r.send(command)
	if r.done != nil {
		<-r.done
	}
	r.started = false
	r.commands = nil
	return err
}

func (r *SyncProgressRenderer) run() {
	defer close(r.done)

	state := rendererState{startedAt: r.now()}
	tick := r.newTicker(r.renderInterval())
	defer tick.Stop()

	for {
		select {
		case now := <-tick.Chan():
			state = r.tick(state, now)
		case command := <-r.commands:
			next, stop, err := r.handleCommand(state, command)
			state = next
			command.reply <- err
			if stop {
				return
			}
		}
	}
}

func (r *SyncProgressRenderer) handleCommand(state rendererState, command rendererCommand) (rendererState, bool, error) {
	switch command.kind {
	case commandStart:
		return r.render(state, true, false)
	case commandStartPhase:
		state.currentPhase = command.phase
		state.phases[command.phase] = phaseActive
		return r.render(state, true, false)
	case commandCompletePhase:
		state.phases[command.phase] = phaseDone
		if command.phase == PhaseData && state.totalTables > 0 {
			state.completedTables = state.totalTables
		}
		return r.render(state, true, false)
	case commandSetPlan:
		state.totalTables = len(command.plan.Tables)
		return state, false, nil
	case commandHandleProgress:
		return r.handleProgress(state, *command.update)
	case commandFailure:
		state, _, renderErr := r.render(state, true, true)
		if renderErr != nil {
			return state, true, renderErr
		}
		if closeErr := r.finishOutput(state); closeErr != nil {
			return state, true, closeErr
		}
		return state, true, fmt.Errorf(
			"sync failed during %s: %w",
			r.phaseName(state.currentPhase),
			command.err,
		)
	case commandCancel:
		state, _, renderErr := r.render(state, true, true)
		if renderErr != nil {
			return state, true, renderErr
		}
		elapsed := r.now().Sub(state.startedAt)
		if closeErr := r.finishOutput(state); closeErr != nil {
			return state, true, closeErr
		}
		return state, true, fmt.Errorf(
			"sync cancelled: %s / %s tables, %s rows copied, elapsed %s: %w",
			formatNumber(int64(state.completedTables)),
			formatNumber(int64(state.totalTables)),
			formatNumber(state.copiedRows),
			formatDuration(elapsed),
			command.err,
		)
	case commandClose:
		if closeErr := r.finishOutput(state); closeErr != nil {
			return state, true, closeErr
		}
		return state, true, nil
	default:
		return state, false, nil
	}
}

func (r *SyncProgressRenderer) tick(state rendererState, now time.Time) rendererState {
	_ = now
	next, _, err := r.render(state, false, false)
	if err != nil {
		return state
	}
	return next
}

func (r *SyncProgressRenderer) handleProgress(state rendererState, update syncpkg.DataProgress) (rendererState, bool, error) {
	switch update.Kind {
	case syncpkg.DataProgressBatchAdjusted:
		if !r.verbose {
			return state, false, nil
		}
		if r.isTTY {
			if err := r.clearTTY(state.lines); err != nil {
				return state, false, err
			}
			state.lines = 0
		}
		_, err := fmt.Fprintf(
			r.out,
			"Batch size adjusted: %d -> %d (%d columns)\n",
			update.ConfiguredBatchSize,
			update.EffectiveBatchSize,
			update.ColumnCount,
		)
		if err != nil {
			return state, false, err
		}
		return r.render(state, true, false)
	case syncpkg.DataProgressTableProgress:
		state = r.updateProgressState(state, update)
		state.completedTables = maxInt(state.completedTables, maxInt(update.TableIndex-1, 0))
		return state, false, nil
	case syncpkg.DataProgressTableComplete, syncpkg.DataProgressDataExcluded:
		state = r.updateProgressState(state, update)
		state.completedTables = maxInt(state.completedTables, update.TableIndex)
		return state, false, nil
	case syncpkg.DataProgressTableStart:
		if update.TableIndex > 0 {
			state.completedTables = maxInt(state.completedTables, maxInt(update.TableIndex-1, 0))
		}
		state.copiedRows = update.TotalRows
		return state, false, nil
	case syncpkg.DataProgressTableFailed:
		state = r.updateProgressState(state, update)
		state.completedTables = maxInt(state.completedTables, maxInt(update.TableIndex-1, 0))
		return state, false, nil
	default:
		return state, false, nil
	}
}

func (r *SyncProgressRenderer) updateProgressState(state rendererState, update syncpkg.DataProgress) rendererState {
	now := r.now()
	if state.startedAt.IsZero() {
		state.startedAt = now
	}

	if update.TotalRows >= state.copiedRows {
		deltaRows := update.TotalRows - state.copiedRows
		if deltaRows > 0 {
			if state.lastRowSampleAt.IsZero() {
				if elapsed := now.Sub(state.startedAt); elapsed > 0 {
					speed := float64(deltaRows) / elapsed.Seconds()
					state.smoothedSpeed = smoothRate(state.smoothedSpeed, speed, speedEMAAlpha)
				}
			} else if deltaTime := now.Sub(state.lastRowSampleAt); deltaTime > 0 {
				speed := float64(deltaRows) / deltaTime.Seconds()
				state.smoothedSpeed = smoothRate(state.smoothedSpeed, speed, speedEMAAlpha)
			}
			state.lastRowSampleAt = now
			state.successfulBatch++
		}
	}

	state.copiedRows = update.TotalRows
	return state
}

func (r *SyncProgressRenderer) render(state rendererState, force bool, final bool) (rendererState, bool, error) {
	now := r.now()
	interval := r.renderInterval()
	if !force && !state.lastRender.IsZero() && now.Sub(state.lastRender) < interval {
		return state, false, nil
	}

	lines := r.linesForDashboard(&state, now)
	if r.isTTY {
		if err := r.clearTTY(state.lines); err != nil {
			return state, false, err
		}
		if _, err := fmt.Fprint(r.out, strings.Join(lines, "\n")); err != nil {
			return state, false, err
		}
		state.lines = len(lines)
	} else {
		if _, err := fmt.Fprintln(r.out, r.nonTTYLine(&state, now)); err != nil {
			return state, false, err
		}
	}

	state.lastRender = now
	return state, false, nil
}

func (r *SyncProgressRenderer) linesForDashboard(state *rendererState, now time.Time) []string {
	percent := clampPercent(r.percentComplete(*state))
	return []string{
		"DBPull Sync",
		"",
		r.phaseLine(state.phases),
		"",
		fmt.Sprintf("%s  %d%%", renderBar(percent, 32), int(math.Round(percent*100))),
		fmt.Sprintf("%s / %s tables", formatNumber(int64(state.completedTables)), formatNumber(int64(state.totalTables))),
		r.rowsLine(*state, now),
		r.timingLine(state, now, percent),
	}
}

func (r *SyncProgressRenderer) nonTTYLine(state *rendererState, now time.Time) string {
	percent := int(math.Round(clampPercent(r.percentComplete(*state)) * 100))
	return fmt.Sprintf(
		"DBPull Sync | %s | %d%% | %s / %s tables | %s rows copied | %s",
		r.phaseLine(state.phases),
		percent,
		formatNumber(int64(state.completedTables)),
		formatNumber(int64(state.totalTables)),
		formatNumber(state.copiedRows),
		r.timingLine(state, now, clampPercent(r.percentComplete(*state))),
	)
}

func (r *SyncProgressRenderer) rowsLine(state rendererState, now time.Time) string {
	parts := []string{fmt.Sprintf("%s rows copied", formatNumber(state.copiedRows))}

	if speed := r.displaySpeed(state, now); speed != "" {
		parts = append(parts, speed)
	}

	return strings.Join(parts, " · ")
}

func (r *SyncProgressRenderer) timingLine(state *rendererState, now time.Time, percent float64) string {
	elapsed := now.Sub(state.startedAt)
	eta := r.displayETA(state, elapsed, percent)
	return "Elapsed " + formatDuration(elapsed) + " · " + eta
}

func (r *SyncProgressRenderer) displaySpeed(state rendererState, now time.Time) string {
	if state.successfulBatch == 0 || now.Sub(state.startedAt) < time.Second {
		return ""
	}

	speed := clampFinite(state.smoothedSpeed)
	if speed <= 0 {
		return ""
	}

	return fmt.Sprintf("%s rows/s", formatNumber(int64(math.Round(speed))))
}

func (r *SyncProgressRenderer) displayETA(state *rendererState, elapsed time.Duration, percent float64) string {
	if state.totalTables == 0 || state.completedTables >= state.totalTables || percent >= 1 {
		state.smoothedETA = 0
		return "ETA 0s"
	}

	if state.completedTables < 5 || elapsed < 2*time.Second {
		return "ETA calculating..."
	}

	rawRate := float64(state.completedTables) / elapsed.Seconds()
	state.smoothedRate = smoothRate(state.smoothedRate, rawRate, rateEMAAlpha)
	if state.smoothedRate <= 0 {
		return "ETA calculating..."
	}

	remainingTables := state.totalTables - state.completedTables
	rawETA := time.Duration(float64(remainingTables)/state.smoothedRate) * time.Second
	if rawETA < 0 {
		rawETA = 0
	}
	state.smoothedETA = smoothDuration(state.smoothedETA, rawETA, rateEMAAlpha)
	return "ETA " + formatDuration(state.smoothedETA)
}

func (r *SyncProgressRenderer) percentComplete(state rendererState) float64 {
	if state.totalTables <= 0 {
		if state.phases[PhaseData] == phaseDone {
			return 1
		}
		return 0
	}

	return float64(state.completedTables) / float64(state.totalTables)
}

func (r *SyncProgressRenderer) phaseLine(phases [6]phaseStatus) string {
	labels := [...]string{"SSH", "Source", "Target", "Plan", "Schema", "Data"}

	parts := make([]string, 0, len(labels))
	for index, label := range labels {
		parts = append(parts, label+" "+phaseSymbol(phases[index]))
	}
	return strings.Join(parts, "  ")
}

func phaseSymbol(status phaseStatus) string {
	switch status {
	case phaseDone:
		return "✓"
	case phaseActive:
		return "◌"
	default:
		return "·"
	}
}

func (r *SyncProgressRenderer) phaseName(phase SyncPhase) string {
	switch phase {
	case PhaseSSH:
		return "SSH"
	case PhaseSource:
		return "Source"
	case PhaseTarget:
		return "Target"
	case PhasePlan:
		return "Plan"
	case PhaseSchema:
		return "Schema"
	case PhaseData:
		return "Data"
	default:
		return "Unknown"
	}
}

func (r *SyncProgressRenderer) finishOutput(state rendererState) error {
	if !r.isTTY || state.lines == 0 {
		return nil
	}
	_, err := fmt.Fprint(r.out, "\n")
	return err
}

func (r *SyncProgressRenderer) clearTTY(lines int) error {
	if lines == 0 {
		return nil
	}

	if _, err := fmt.Fprint(r.out, "\r\x1b[2K"); err != nil {
		return err
	}
	for i := 1; i < lines; i++ {
		if _, err := fmt.Fprint(r.out, "\x1b[1A\r\x1b[2K"); err != nil {
			return err
		}
	}
	return nil
}

func (r *SyncProgressRenderer) renderInterval() time.Duration {
	if r.isTTY {
		return ttyRenderInterval
	}
	return nonTTYRenderInterval
}

func smoothRate(previous, current, alpha float64) float64 {
	current = clampFinite(current)
	if current <= 0 {
		return clampFinite(previous)
	}
	if previous <= 0 {
		return current
	}
	return alpha*current + (1-alpha)*previous
}

func smoothDuration(previous, current time.Duration, alpha float64) time.Duration {
	if current <= 0 {
		return 0
	}
	if previous <= 0 {
		return current
	}
	smoothed := alpha*float64(current) + (1-alpha)*float64(previous)
	if smoothed < 0 || math.IsNaN(smoothed) || math.IsInf(smoothed, 0) {
		return current
	}
	return time.Duration(smoothed)
}

func clampFinite(value float64) float64 {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func clampPercent(percent float64) float64 {
	if percent < 0 {
		return 0
	}
	if percent > 1 {
		return 1
	}
	return percent
}

func renderBar(percent float64, width int) string {
	percent = clampPercent(percent)

	filled := int(math.Round(percent * float64(width)))
	if filled > width {
		filled = width
	}

	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}

	if duration < time.Minute {
		seconds := int(duration.Round(time.Second) / time.Second)
		return fmt.Sprintf("%ds", seconds)
	}
	if duration < time.Hour {
		minutes := int(duration / time.Minute)
		seconds := int((duration % time.Minute).Round(time.Second) / time.Second)
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}

	hours := int(duration / time.Hour)
	minutes := int((duration % time.Hour) / time.Minute)
	return fmt.Sprintf("%dh%02dm", hours, minutes)
}

func formatNumber(number int64) string {
	negative := number < 0
	if negative {
		number = -number
	}

	text := fmt.Sprintf("%d", number)
	if len(text) <= 3 {
		if negative {
			return "-" + text
		}
		return text
	}

	var parts []string
	for len(text) > 3 {
		parts = append([]string{text[len(text)-3:]}, parts...)
		text = text[:len(text)-3]
	}
	parts = append([]string{text}, parts...)
	if negative {
		return "-" + strings.Join(parts, ",")
	}
	return strings.Join(parts, ",")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
