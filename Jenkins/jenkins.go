package jenkins

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

type Jenkins struct {
	RootURL  string
	User     string
	Pass     string
	id       string
	Callback func()
}

const (
	Running int = iota
	Done
	Error
)

func (this *Jenkins) InvokeJob(jobName string, jobParameters map[string]string) error {
	url := this.RootURL + "/job/" + jobName + "/buildWithParameters?"
	for key, value := range jobParameters {
		url = url + "&" + key + "=" + value
	}

	logrus.WithField("Задание", jobName).Info("Выполняем задание")
	err, _ := callREST(url, this.User, this.Pass)
	return err
}

// Нет смысла в этом методе т.к. jenkins не сразу берет добавленое задание в работу
// новый lastBuild появится только когда задание начнет выполняться, т.е. если добавить новое задание
// и запросить GetLastJobID вернется предыдущее задание
func (this *Jenkins) GetLastJobID(jobName string) {
	// defer func() {
	// 	if err := recover(); err != nil {
	// 		result = ""
	// 	}
	// }()

	url := this.RootURL + "/job/" + jobName + "/api/xml?xpath=//lastBuild/number/text()"
	if err, result := callREST(url, this.User, this.Pass); err == nil {
		this.id = result
	}
}

func GetJobStatus(RootURL, jobName, User, Pass string) int {
	url := RootURL + "/job/" + jobName + "/api/xml?xpath=/workflowJob/color/text()"
	if err, result := callREST(url, User, Pass); err == nil {
		// только на цвет получилось завязаться, другого признака не нашел
		switch result {
		case "blue":
			return Done
		case "blue_anime":
			return Running
		case "red":
			return Error
		}
	}

	return -1
}

func callREST(url, User, Pass string) (error, string) {
	logrus.Infof("Вызываем URL %v", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		logrus.WithField("URL", url).
			Errorf("Произошла ошибка при вызове задания: %v", err)
		return err, ""
	}
	req.SetBasicAuth(User, Pass)

	client := &http.Client{Timeout: time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		logrus.WithField("URL", url).
			Errorf("Произошла ошибка при выполнении задания: %v", err)
		return err, ""
	}
	if resp != nil {
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			logrus.WithField("URL", url).
				Errorf("Произошла ошибка при чтении Body: %v", err)
			return err, ""
		}
		if !(resp.StatusCode >= http.StatusOK && resp.StatusCode <= http.StatusIMUsed) {
			logrus.WithField("url", url).Errorf("Код ответа %v", resp.StatusCode)
			return fmt.Errorf("Код ответа %v", resp.StatusCode), ""
		}

		return nil, string(body)
	} else {
		logrus.WithField("url", url).Error("Не получен ответ")
		return fmt.Errorf("Не получен ответ"), ""
	}

}
