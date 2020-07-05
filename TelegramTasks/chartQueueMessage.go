package telegram

import (
	n "TelegramBot/Net"
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

type QueueMessageData struct {
	Count     int                 `json:"count"`
	Data      []*QueueMessageData `json:"data"`
	Direction string              `json:"direction"`
	Base      string              `json:"base"`
}

type chartQueueData struct {
	name  string
	count float64
}

type chartQueueMessage struct {
	width, height vg.Length
}

func (this *chartQueueMessage) Build() (string, error) {
	data, _ := this.getGata()

	if len(data) == 0 {
		return "", errorNotData
	}

	// коэффициенты выявлены эмпирически
	minwidth := float64(200)
	this.width = vg.Length(math.Max(minwidth, float64(len(data)*65)))
	this.height = vg.Length(500)

	group := plotter.Values{}
	names := []string{}
	for _, item := range data {
		group = append(group, item.count1)
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

func (this *chartQueueMessage) getGata() (result []*chartData, max float64) {

	// не map т.к. в мапе данные не упорядоченые, а нам важен порядок
	result = make([]*chartData, 0)

	url := Confs.Charts.Services["InfobasesDiscovery"]
	User := Confs.Charts.Login
	Pass := Confs.Charts.Password
	data := map[string][]map[string]string{}

	netU := new(n.NetUtility).Construct(url, User, Pass)
	if JSONdata, err := netU.CallHTTP(http.MethodGet, time.Minute, nil); err != nil {
		return result, 0
	} else {
		json.Unmarshal([]byte(JSONdata), &data)
	}

	for _, base := range data["data"] {
		baseName := base[`{#INFOBASE}`]
		url := Confs.Charts.Services["QueueMessage"] + "?base=" + baseName

		netU := new(n.NetUtility).Construct(url, User, Pass)
		JSONdata, _ := netU.CallHTTP(http.MethodGet, time.Second*10, nil)
		if len(JSONdata) == 0 {
			continue
		}

		nodeData := []*QueueMessageData{}
		json.Unmarshal([]byte(JSONdata), &nodeData)
		if len(nodeData) == 0 {
			continue
		}

		//result = append(result, &chartData{name: baseName, count: float64(nodeData.Count)})
	}

	// сортируем по значению, это нужно что б на графике легенду не закрывало
	sort.Slice(result, func(i, j int) bool {
		max = math.Max(math.Max(max, result[i].count1), result[j].count1)             // в принципе это рудимент
		return result[i].count1+result[i].count2 >= result[j].count1+result[j].count2 // сортируем по общему кольчеству метрик
	})
	return result, max
}
