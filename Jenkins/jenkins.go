package jenkins

import (
	n "1C/Net"
	"fmt"
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
	logrus.Debug("Создаем объект для работы с Jenkins")
	this.jobName = jobName

	return this
}

func (this *Jenkins) InvokeJob(jobParameters map[string]string) error {
	logrus.Debug(fmt.Sprintf("Выполняем задание %v", this.jobName))

	url := this.RootURL + "/job/" + this.jobName + "/buildWithParameters?"
	for key, value := range jobParameters {
		url = url + key + "=" + value + "&"
	}
	url = url[:len(url)-1]

	logrus.WithField("Задание", this.jobName).Info("Выполняем задание")

	netU := new(n.NetUtility).Construct(url, this.User, this.Pass)
	if this.Token != "" {
		netU.Header["token"] = this.Token
	}
	_, err := netU.CallHTTP(http.MethodPost, time.Minute)
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
	netU := new(n.NetUtility).Construct(url, this.User, this.Pass)
	if result, err := netU.CallHTTP(http.MethodGet, time.Minute); err == nil {
		this.jobID = result
	}
}

func (this *Jenkins) GetJobStatus() int {
	logrus.Debug(fmt.Sprintf("Получаем статус задания %v", this.jobName))

	url := this.RootURL + "/job/" + this.jobName + "/api/xml" // ?xpath=/workflowJob/color/text() //конкретный инстенс дженкинса с xpath не работает, ошибка jenkins primitive XPath result sets forbidden
	netU := new(n.NetUtility).Construct(url, this.User, this.Pass)
	if result, err := netU.CallHTTP(http.MethodGet, time.Minute); err == nil {
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

func (this *Jenkins) CheckStatus(FSuccess, FEror, FTimeOut func()) {
	logrus.Debug(fmt.Sprintf("Отслеживаем статус задания %v", this.jobName))

	var once sync.Once
	timeout := time.NewTicker(time.Minute * 15)
	timer := time.NewTicker(time.Second * 10)
	for range timer.C {
		logrus.WithField("Значение", timer.C).Debug("Итерация таймера")

		status := this.GetJobStatus()
		switch status {
		case Error:
			logrus.Debug("Задание выполнено с ошибкой")
			FEror()
			timer.Stop()
			timeout.Stop()
		case Done:
			logrus.Debug("Задание выполнено успешно")
			FSuccess()
			timer.Stop()
			timeout.Stop()
		case Undefined:
			// Если у нас статус неопределен, запускаем таймер таймаута, если при запущеном таймере статус поменяется на определенный, мы остановим таймер
			// таймер нужно запустить один раз
			once.Do(func() {
				logrus.Debug("Старт timeout")
				go func() {
					// используется таймер, а не слип например потому, что должна быть возможность прервать из вне, да можно наверное было бы и через контекст, но зачем так заморачиваться
					<-timeout.C // читаем из канала, нам нужно буквально одного события
					FTimeOut()
					timer.Stop()
					timeout.Stop()
					logrus.Debug("Стоп timeout")
				}()
			})
		}
	}
}
