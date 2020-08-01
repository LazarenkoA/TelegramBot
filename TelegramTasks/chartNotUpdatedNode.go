package telegram

import (
	n "github.com/LazarenkoA/TelegramBot/Net"
	"encoding/json"
	"io/ioutil"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

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

// type chartData struct {
// 	name  string
// 	count float64
// }

type chartNotUpdatedNode struct {
	width, height vg.Length
}

func (this *chartNotUpdatedNode) Build() (result []string, err error) {
	data, maxvalue := this.getGata()
	chanData := make(chan []*chartData, 0)
	result = []string{}

	if len(data) == 0 {
		return []string{}, errorNotData
	}

	wg := new(sync.WaitGroup)
	wg.Add(1)
	go func() {
		defer wg.Done()

		for datapart := range chanData {
			// коэффициенты выявлены эмпирически
			minwidth := float64(200)
			this.width = vg.Length(math.Max(minwidth, float64(len(datapart)*65)))
			this.height = vg.Length(500)

			group1 := plotter.Values{}
			group2 := plotter.Values{}
			names := []string{}
			for _, item := range datapart {
				group1 = append(group1, item.count1)
				group2 = append(group2, item.count2)
				names = append(names, item.name)
			}

			p, err := plot.New()
			if err != nil {
				continue
			}
			//p.Title.Text = "Количество не обновленных областей"
			p.Y.Label.Text = "Количество"
			p.Y.Max = float64(maxvalue + maxvalue*0.06) // +5% что б был отступ

			w := vg.Points(20)

			bar1, err := plotter.NewBarChart(group1, w)
			if err != nil {
				continue
			}
			bar1.LineStyle.Width = vg.Length(0)
			bar1.Color = plotutil.Color(0)

			bar2, err := plotter.NewBarChart(group2, w)
			if err != nil {
				continue
			}
			bar2.LineStyle.Width = vg.Length(0)
			bar2.Color = plotutil.Color(1)
			bar2.Offset = -w

			p.Add(bar1, bar2)
			p.Legend.Add("Не обновленные области", bar1)
			p.Legend.Add("Проблемы метаданных", bar2)
			p.Legend.Top = true

			p.NominalX(names...)

			tmpDir, err := ioutil.TempDir("", "")
			if err != nil {
				continue
			}
			result = append(result, filepath.Join(tmpDir, "chart.png"))

			if err := p.Save(this.width, this.height, result[len(result)-1]); err != nil {
				continue
			}
		}
	}()

	// разбиваем слайс на части, если пытаться уместить в один chart будет мелко и не видно
	countparts := 10
	if len(data) <= countparts {
		chanData <- data
	} else {
		for len(data) > 0 {
			countparts = int(math.Min(float64(countparts), float64(len(data)))) % int(math.Max(float64(countparts), float64(len(data))))
			chanData <- data[:countparts]
			data = data[countparts:]
		}
	}
	close(chanData)

	wg.Wait()
	return result, nil
}

func (this *chartNotUpdatedNode) getGata() (result []*chartData, max float64) {
	// не map т.к. в мапе данные не упорядоченые, а нам важен порядок
	result = make([]*chartData, 0)

	url := Confs.Charts.Services["InfobasesDiscovery"]
	User := Confs.Charts.Login
	Pass := Confs.Charts.Password
	data := new(MonitoringData)

	netU := new(n.NetUtility).Construct(url, User, Pass)
	if JSONdata, err := netU.CallHTTP(http.MethodGet, time.Minute, nil); err != nil {
		return result, 0
	} else {
		json.Unmarshal([]byte(JSONdata), data)
	}

	get := func(url string) *MonitoringData {
		netU := new(n.NetUtility).Construct(url, User, Pass)
		JSONdata, err := netU.CallHTTP(http.MethodGet, time.Second*30, nil)
		if err != nil {
			logrus.WithError(err).WithField("URL", url).Error()
		}
		if len(JSONdata) == 0 {
			return new(MonitoringData)
		}

		nodeData := new(MonitoringData)
		if err := json.Unmarshal([]byte(JSONdata), nodeData); err != nil || nodeData.Count == 0 {
			return new(MonitoringData)
		}

		return nodeData
	}

	wg := new(sync.WaitGroup)
	for _, base := range data.Base {
		baseName := base[`{#INFOBASE}`]
		data := &chartData{name: baseName}
		wg.Add(2)

		go func() {
			defer wg.Done()
			url := Confs.Charts.Services["NotUpdatedZones"] + "?base=" + baseName
			if nodeData := get(url); nodeData.Count > 0 {
				data.count1 = float64(nodeData.Count)
			}
		}()

		go func() {
			defer wg.Done()
			url = Confs.Charts.Services["BadMetadataIdentificators"] + "?base=" + baseName
			if nodeData := get(url); nodeData.Count > 0 {
				data.count2 = float64(nodeData.Count)
			}
		}()

		result = append(result, data)
	}
	wg.Wait()

	// удаляем пусые даые
	for i := -(len(result) - 1); i <= 0; i++ {
		if result[-i].count1 == 0 && result[-i].count2 == 0 {
			result = append(result[:-i], result[-i+1:]...)
		}
	}

	// сортируем по значению, это нужно что б на графике легенду не закрывало
	sort.Slice(result, func(i, j int) bool {
		max = math.Max(math.Max(math.Max(math.Max(max, result[i].count1), result[j].count1), result[i].count2), result[j].count2)
		return result[i].count1+result[i].count2 >= result[j].count1+result[j].count2 // сортируем по общему кольчеству метрик
	})
	return result, max
}
