package telegram

import (
	n "1C/Net"
	"encoding/json"
	"io/ioutil"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"time"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
)

type MonitoringData struct {
	Base  []map[string]string `json:"data"`
	Count int                 `json:"Count"`
	List  []int               `json:"List"`
}

type chartData struct {
	name  string
	count float64
}

type chartNotUpdatedNode struct {
	width, height vg.Length
}

func (this *chartNotUpdatedNode) Build() (string, error) {
	data, _ := this.getGata()

	// коэффициенты выявлены эмпирически
	this.width = vg.Length(len(data) * 65)
	this.height = vg.Length(500)

	group := plotter.Values{}
	names := []string{}
	for _, item := range data {
		group = append(group, item.count)
		names = append(names, item.name)
	}

	p, err := plot.New()
	if err != nil {
		return "", err
	}
	p.Title.Text = "Количество не обновленных областей"
	p.Y.Label.Text = "Количество"

	w := vg.Points(20)

	bars, err := plotter.NewBarChart(group, w)
	if err != nil {
		return "", err
	}
	bars.LineStyle.Width = vg.Length(0)
	bars.Color = plotutil.Color(0)
	//bars.Offset = -w

	p.Add(bars)
	//p.Legend.Add("База", bars)
	//p.Legend.Top = true

	p.NominalX(names...)

	tmpDir, err := ioutil.TempDir("", "")
	if err != nil {
		return "", err
	}
	chartFile := filepath.Join(tmpDir, "chart.png")

	if err := p.Save(this.width, this.height, chartFile); err != nil {
		return "", err
	}

	return chartFile, nil
}

func (this *chartNotUpdatedNode) getGata() (result []*chartData, max float64) {
	// не map т.к. в мапе данные не упорядоченые, а нам важен порядок
	result = make([]*chartData, 0)

	url := Confs.Charts.Services["InfobasesDiscovery"]
	User := Confs.Charts.Login
	Pass := Confs.Charts.Password
	data := new(MonitoringData)

	netU := new(n.NetUtility).Construct(url, User, Pass)
	if JSONdata, err := netU.CallHTTP(http.MethodGet, time.Minute); err != nil {
		return result, 0
	} else {
		json.Unmarshal([]byte(JSONdata), data)
	}

	for _, base := range data.Base {
		baseName := base[`{#INFOBASE}`]
		url := Confs.Charts.Services["NotUpdatedZones"] + "?base=" + baseName

		netU := new(n.NetUtility).Construct(url, User, Pass)
		JSONdata, _ := netU.CallHTTP(http.MethodGet, time.Second*10)
		if len(JSONdata) == 0 {
			continue
		}

		nodeData := new(MonitoringData)
		json.Unmarshal([]byte(JSONdata), nodeData)
		if nodeData.Count == 0 {
			continue
		}
		result = append(result, &chartData{name: baseName, count: float64(nodeData.Count)})
	}

	// сортируем по значению, это нужно что б на графике легенду не закрывало
	sort.Slice(result, func(i, j int) bool {
		max = math.Max(max, result[i].count)
		max = math.Max(max, result[j].count)
		return result[i].count >= result[j].count
	})
	return result, max
}
