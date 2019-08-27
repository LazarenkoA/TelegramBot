package jenkins

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	xmlpath "gopkg.in/xmlpath.v2"
)

type Jenkins struct {
	RootURL string
	User    string
	Pass    string
	Token   string
	jobID   string
	jobName string
	//Callback func()
}

const (
	Running int = iota
	Done
	Error
	Undefined
)

func (this *Jenkins) Create(jobName string) *Jenkins {
	this.jobName = jobName

	return this
}

func (this *Jenkins) InvokeJob(jobParameters map[string]string) error {
	url := this.RootURL + "/job/" + this.jobName + "/buildWithParameters?"
	for key, value := range jobParameters {
		url = url + key + "=" + value + "&"
	}
	url = url[:len(url)-1]

	logrus.WithField("Задание", this.jobName).Info("Выполняем задание")
	err, _ := callREST("POST", url, this.User, this.Pass, this.Token)
	return err
}

// Нет смысла в этом методе т.к. jenkins не сразу берет добавленое задание в работу
// новый lastBuild появится только когда задание начнет выполняться, т.е. если добавить новое задание
// и запросить GetLastJobID вернется предыдущее задание
func (this *Jenkins) GetLastJobID() {
	// defer func() {
	// 	if err := recover(); err != nil {
	// 		result = ""
	// 	}
	// }()

	url := this.RootURL + "/job/" + this.jobName + "/api/xml?xpath=//lastBuild/number/text()"
	if err, result := callREST("GET", url, this.User, this.Pass, ""); err == nil {
		this.jobID = result
	}
}

func (this *Jenkins) GetJobStatus() int {
	url := this.RootURL + "/job/" + this.jobName + "/api/xml" // ?xpath=/workflowJob/color/text() //конкретный инстенс дженкинса с xpath не работает, ошибка jenkins primitive XPath result sets forbidden
	if err, result := callREST("GET", url, this.User, this.Pass, ""); err == nil {
		xmlroot, xmlerr := xmlpath.Parse(strings.NewReader(result))
		if xmlerr != nil {
			logrus.WithField("URL", url).Errorf("Ошибка чтения xml %q", xmlerr.Error())
			return -1
		}

		// только на цвет получилось завязаться, другого признака не нашел
		color := xmlpath.MustCompile("/workflowJob/color/text()")
		if value, ok := color.String(xmlroot); ok {
			switch value {
			case "blue":
				return Done
			case "blue_anime":
				return Running
			case "red":
				return Error
			default:
				return Undefined
			}
		}

	}

	return -1
}

func callREST(method string, url, User, Pass, Token string) (error, string) {
	logrus.Infof("Вызываем URL %v", url)

	req, err := http.NewRequest(method, url, nil)
	if Token != "" {
		req.Header.Add("token", Token)
	}

	// for key, value := range postParam {
	// 	req.Header.Add(key, value)
	// }

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

func (this *Jenkins) CheckStatus(FSuccess, FEror, FTimeOut func()) {
	var once sync.Once
	timeout := time.NewTicker(time.Minute * 5)
	timer := time.NewTicker(time.Second * 10)
	for range timer.C {
		status := this.GetJobStatus()
		switch status {
		case Error:
			FEror()
			timer.Stop()
			timeout.Stop()
		case Done:
			FSuccess()
			timer.Stop()
			timeout.Stop()
		case Undefined:
			// Если у нас статус неопределен, запускаем таймер таймаута, если при запущеном таймере статус поменяется на определенный, мы остановим таймер
			// таймер нужно запустить один раз
			once.Do(func() {
				go func() {
					// используется таймер, а не слип например потому, что должна быть возможность прервать из вне, да можно наверное было бы и через контекст, но зачем так заморачиваться
					<-timeout.C // читаем из канала, нам нужно буквально одного события
					FTimeOut()
					timer.Stop()
					timeout.Stop()
				}()
			})
		}
	}
}
