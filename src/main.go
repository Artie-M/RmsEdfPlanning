package main

import (
	"encoding/csv"
	"fmt"
	"image/color"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
)

type Task struct {
	Name string
	C    int
	T    int
	D    int
}

type ScheduleSegment struct {
	TaskName string
	TaskIdx  int
	Start    int
	End      int
}

func loadTasks(filePath string) ([]Task, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть файл %s: %w", filePath, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения CSV: %w", err)
	}

	var tasks []Task
	for i, row := range records {
		if len(row) < 4 {
			continue
		}

		if i == 0 {
			if _, err := strconv.Atoi(strings.TrimSpace(row[1])); err != nil {
				continue
			}
		}

		name := strings.TrimSpace(row[0])
		c, errC := strconv.Atoi(strings.TrimSpace(row[1]))
		t, errT := strconv.Atoi(strings.TrimSpace(row[2]))
		d, errD := strconv.Atoi(strings.TrimSpace(row[3]))
		if errC != nil || errT != nil || errD != nil {
			return nil, fmt.Errorf("некорректные числовые значения в строке %d", i+1)
		}
		if c <= 0 || t <= 0 || d <= 0 {
			return nil, fmt.Errorf("параметры C, T, D должны быть положительными (строка %d)", i+1)
		}

		tasks = append(tasks, Task{
			Name: name,
			C:    c,
			T:    t,
			D:    d,
		})
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("файл %s не содержит корректных задач", filePath)
	}

	return tasks, nil
}

func sortTasksByRMS(tasks []Task) []Task {
	sorted := make([]Task, len(tasks))
	copy(sorted, tasks)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].T < sorted[j].T
	})
	return sorted
}

func utilization(tasks []Task) float64 {
	var u float64
	for _, task := range tasks {
		u += float64(task.C) / float64(task.T)
	}
	return u
}

func liu_layland_bound(n int) float64 {
	if n <= 0 {
		return 0
	}
	nf := float64(n)
	return nf * (math.Pow(2.0, 1.0/nf) - 1.0)
}

func response_time(task Task, higher []Task) int {
	r := task.C
	for {
		w := task.C
		for _, hp := range higher {
			w += int(math.Ceil(float64(r)/float64(hp.T))) * hp.C
		}
		if w == r {
			return r
		}

		if w > 1000000 {
			return w
		}
		r = w
	}
}

func rms_schedulable(tasks []Task) bool {
	sorted := sortTasksByRMS(tasks)
	u := utilization(sorted)
	bound := liu_layland_bound(len(sorted))

	implicitDeadlines := true
	for _, t := range sorted {
		if t.D != t.T {
			implicitDeadlines = false
			break
		}
	}
	if implicitDeadlines && u <= bound {
		return true
	}

	for i, task := range sorted {
		r := response_time(task, sorted[:i])
		if r > task.D {
			return false
		}
	}
	return true
}

func edf_schedulable(tasks []Task) bool {
	return utilization(tasks) <= 1.0+1e-9
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func lcm(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	return (a * b) / gcd(a, b)
}

func hyperperiod(tasks []Task) int {
	h := 1
	for _, t := range tasks {
		h = lcm(h, t.T)
	}
	return h
}

func gantt(tasks []Task, interval int) []ScheduleSegment {
	sorted := sortTasksByRMS(tasks)
	n := len(sorted)
	rem := make([]int, n)
	var segments []ScheduleSegment

	for t := 0; t < interval; t++ {

		for i := 0; i < n; i++ {
			if t%sorted[i].T == 0 {
				rem[i] += sorted[i].C
			}
		}

		activeIdx := -1
		for i := 0; i < n; i++ {
			if rem[i] > 0 {
				activeIdx = i
				break
			}
		}

		if activeIdx != -1 {
			rem[activeIdx]--
			if len(segments) > 0 && segments[len(segments)-1].TaskIdx == activeIdx && segments[len(segments)-1].End == t {
				segments[len(segments)-1].End = t + 1
			} else {
				segments = append(segments, ScheduleSegment{
					TaskName: sorted[activeIdx].Name,
					TaskIdx:  activeIdx,
					Start:    t,
					End:      t + 1,
				})
			}
		}
	}

	chartPath := resolveChartPath()
	if err := renderGanttChart(sorted, segments, interval, chartPath); err != nil {
		log.Printf("Ошибка при сохранении графика: %v\n", err)
	}

	return segments
}

type intervalBars struct {
	segments []ScheduleSegment
	numTasks int
	colors   []color.RGBA
}

func (b *intervalBars) Plot(c draw.Canvas, plt *plot.Plot) {
	trX, trY := plt.Transforms(&c)
	barHalfHeight := vg.Points(11)

	for _, seg := range b.segments {

		yVal := float64(b.numTasks - 1 - seg.TaskIdx)
		xMin := trX(float64(seg.Start))
		xMax := trX(float64(seg.End))
		yCenter := trY(yVal)

		rect := vg.Path{}
		rect.Move(vg.Point{X: xMin, Y: yCenter - barHalfHeight})
		rect.Line(vg.Point{X: xMax, Y: yCenter - barHalfHeight})
		rect.Line(vg.Point{X: xMax, Y: yCenter + barHalfHeight})
		rect.Line(vg.Point{X: xMin, Y: yCenter + barHalfHeight})
		rect.Close()

		col := b.colors[seg.TaskIdx%len(b.colors)]
		c.SetColor(col)
		c.Fill(rect)

		c.SetColor(color.RGBA{R: 35, G: 45, B: 60, A: 255})
		c.SetLineWidth(vg.Points(0.8))
		c.Stroke(rect)
	}
}

func (b *intervalBars) DataRange() (xmin, xmax, ymin, ymax float64) {
	xmin, xmax = 0, 1
	for _, seg := range b.segments {
		if float64(seg.End) > xmax {
			xmax = float64(seg.End)
		}
	}
	return xmin, xmax, -0.6, float64(b.numTasks) - 0.4
}

func renderGanttChart(sortedTasks []Task, segments []ScheduleSegment, interval int, outPath string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}

	p := plot.New()
	p.Title.Text = fmt.Sprintf("Диаграмма выполнения задач по RMS на интервале [0, %d] мс", interval)
	p.X.Label.Text = "Время, мс"
	p.Y.Label.Text = "Задачи (сверху вниз по убыванию приоритета)"
	p.X.Min = 0
	p.X.Max = float64(interval)
	p.Y.Min = -0.6
	p.Y.Max = float64(len(sortedTasks)) - 0.4

	yTicks := make([]plot.Tick, len(sortedTasks))
	for i, task := range sortedTasks {
		yTicks[i] = plot.Tick{
			Value: float64(len(sortedTasks) - 1 - i),
			Label: fmt.Sprintf("%s (T=%d)", task.Name, task.T),
		}
	}
	p.Y.Tick.Marker = plot.ConstantTicks(yTicks)

	step := 1
	if interval > 50 {
		step = interval / 25
	}
	var xTicks []plot.Tick
	for t := 0; t <= interval; t += step {
		xTicks = append(xTicks, plot.Tick{
			Value: float64(t),
			Label: strconv.Itoa(t),
		})
	}
	p.X.Tick.Marker = plot.ConstantTicks(xTicks)

	grid := plotter.NewGrid()
	grid.Vertical.Color = color.RGBA{R: 220, G: 225, B: 230, A: 255}
	grid.Horizontal.Color = color.RGBA{R: 235, G: 238, B: 242, A: 255}
	p.Add(grid)

	palette := []color.RGBA{
		{R: 46, G: 125, B: 185, A: 230},
		{R: 56, G: 168, B: 115, A: 230},
		{R: 230, G: 138, B: 46, A: 230},
		{R: 142, G: 92, B: 178, A: 230},
		{R: 214, G: 78, B: 82, A: 230},
	}

	bars := &intervalBars{
		segments: segments,
		numTasks: len(sortedTasks),
		colors:   palette,
	}
	p.Add(bars)

	width := vg.Points(760)
	height := vg.Points(float64(140 + len(sortedTasks)*45))
	return p.Save(width, height, outPath)
}

func resolveCSVPath() string {
	candidates := []string{
		filepath.Join("data", "tasks.csv"),
		filepath.Join("..", "data", "tasks.csv"),
		"tasks.csv",
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return filepath.Join("data", "tasks.csv")
}

func resolveChartPath() string {
	if _, err := os.Stat("src"); err == nil {
		return filepath.Join("src", "charts", "chart.png")
	}
	return filepath.Join("charts", "chart.png")
}

func printTasksTable(sortedTasks []Task) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Задача\tC\tT\tD\tR\tУложилась")
	fmt.Fprintln(w, "------\t-----\t------\t------\t------\t------")

	for i, task := range sortedTasks {
		r := response_time(task, sortedTasks[:i])
		meetsDeadline := "Да"
		if r > task.D {
			meetsDeadline = "Нет"
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%s\n",
			task.Name, task.C, task.T, task.D, r, meetsDeadline)
	}
	w.Flush()
}

func main() {
	csvPath := resolveCSVPath()
	tasks, err := loadTasks(csvPath)
	if err != nil {
		log.Fatalf("Ошибка загрузки данных: %v", err)
	}

	sortedTasks := sortTasksByRMS(tasks)
	n := len(sortedTasks)

	fmt.Printf("Входной файл: %s (количество задач n = %d)\n", csvPath, n)
	printTasksTable(sortedTasks)
	fmt.Println("\n--- ЗАКЛЮЧЕНИЕ ---")

	u := utilization(sortedTasks)
	llBound := liu_layland_bound(n)
	rmsOK := rms_schedulable(sortedTasks)
	edfOK := edf_schedulable(sortedTasks)
	hp := hyperperiod(sortedTasks)

	fmt.Printf("Коэффициент загрузки: U = %.3f\n", u)

	llStatus := "не выполнен (U > границы, требуется проверка по R)"
	if u <= llBound {
		llStatus = "выполнен (U <= границы)"
	}
	rmsVerdict := "набор планируем"
	if !rmsOK {
		rmsVerdict = "набор НЕ планируем"
	}
	fmt.Printf("RMS (граница Liu & Layland для n = %d равна %.3f): тест %s — %s.\n", n, llBound, llStatus, rmsVerdict)

	edfVerdict := "U <= 1 — набор планируем"
	if !edfOK {
		edfVerdict = "U > 1 — набор НЕ планируем"
	}
	fmt.Printf("EDF: U = %.3f (%s).\n", u, edfVerdict)

	gantt(sortedTasks, hp)
	fmt.Printf("Гиперпериод: H = %d мс. Диаграмма выполнения RMS сохранена в ./src/charts/chart.png", hp)
}
