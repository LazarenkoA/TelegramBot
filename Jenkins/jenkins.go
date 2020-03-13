package jenkins

import (
	n "TelegramBot/Net"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	xmlpath "gopkg.in/xmlpath.v2"
)

type Jenkins struct {
	RootURL  string
	User     string
	Pass     string
	Token    string
	JobID    string
	jobName  string
	jobCount int
	jobURLs  map[string]bool
	errors   []string

	//Callback func()
}

var errTimeOut error = errors.New("Прервано по таймауту")

const (
	Running int = iota
	Done
	Error
	Undefined
)

func (this *Jenkins) Create(jobName string) *Jenkins {
	logrus.Debug("Создаем объект для работы с Jenkins")

	rand.Seed(time.Now().Unix())
	this.jobName = jobName
	this.JobID = fmt.Sprint(rand.Intn(10000-1000) + 1000) // от 1000 - 10000
	this.jobURLs = make(map[string]bool, 0)

	return this
}

func (this *Jenkins) InvokeJob(jobParameters map[string]string) error {
	logrus.WithField("Parameters", jobParameters).Debug(fmt.Sprintf("Выполняем задание jenkins %v", this.jobName))
	this.jobCount++

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

func (this *Jenkins) containsGroup(jobURL string) bool {
	netU := new(n.NetUtility).Construct(jobURL, this.User, this.Pass)
	if result, err := netU.CallHTTP(http.MethodGet, time.Minute); err == nil {
		xmlroot, xmlerr := xmlpath.Parse(strings.NewReader(result))
		if xmlerr != nil {
			logrus.WithField("URL", jobURL).Errorf("Ошибка чтения xml %q", xmlerr.Error())
			return false
		}

		result := xmlpath.MustCompile("/workflowRun/displayName/text()")
		if value, ok := result.String(xmlroot); ok && strings.Contains(value, fmt.Sprint(this.JobID)) {
			return true
		} else {
			return false
		}
	} else {
		return false
	}
}

func (this *Jenkins) checkJobStatus(jobURL string, chanErr chan error) {
	netU := new(n.NetUtility).Construct(jobURL, this.User, this.Pass)

	f := func() (result bool) {
		defer func() {
			if err := recover(); err != nil {
				chanErr <- fmt.Errorf("%v", err)
			}
		}()

		logrus.WithField("url", jobURL).Debug("Получаем XML")
		if result, err := netU.CallHTTP(http.MethodGet, time.Minute); err == nil {
			xmlroot, xmlerr := xmlpath.Parse(strings.NewReader(result))
			if xmlerr != nil {
				logrus.WithField("URL", jobURL).Errorf("Ошибка чтения xml %q", xmlerr.Error())
				chanErr <- xmlerr
				return true // true - зачит первать выполнение
			}

			displayNamePath := xmlpath.MustCompile("/workflowRun/displayName/text()")
			displayName, _ := displayNamePath.String(xmlroot)

			result := xmlpath.MustCompile("/workflowRun/result/text()")

			value, _ := result.String(xmlroot)
			logrus.WithField("url", jobURL).Debugf("Статус %q", value)
			if strings.ToUpper(value) == "SUCCESS" {
				return true
			} else if strings.ToUpper(value) == "FAILURE" {
				chanErr <- fmt.Errorf("задание %q завершилось с ошибой", displayName)
				return true
			} else if strings.ToUpper(value) == "ABORTED" {
				chanErr <- fmt.Errorf("задание %q было прервано", displayName)
				return true
			}
		} else {
			chanErr <- err
			return true
		}

		return false
	}

	if err := runWithTimeout(time.Hour, f); err != nil {
		chanErr <- err
	}
}

// фукция будет выполнят передаую в нее функцию пстояно пока а не вернет true или пока не завершится таймаут
func runWithTimeout(duration time.Duration, f func() bool) error {
	logrus.WithField("Timeout", duration).Debug("Старт timeout")

	ctx, cancel := context.WithCancel(context.Background())
	timeout := time.NewTicker(duration)
	go func() {
		<-timeout.C // читаем из канала, нам нужно буквально одного события
		cancel()
	}()

	delay := time.NewTicker(time.Second * 10)

	i := 0
exit:
	for {
		i++
		logrus.Debugf("runWithTimeout. Выполняем метод, попытка %v", i)

		select {
		case <-ctx.Done():
			timeout.Stop()
			delay.Stop()
			return errTimeOut
		default:
			if f() {
				timeout.Stop()
				delay.Stop()
				break exit
			}
		}

		<-delay.C
	}

	return nil
}

func (this *Jenkins) CheckStatus(FSuccess, FTimeOut func(), FEror func(string)) {
	logrus.Debug(fmt.Sprintf("Отслеживаем статус задания %v", this.jobName))
	chanJob := make(chan string, 5)
	chanErr := make(chan error)
	wg := new(sync.WaitGroup)

	go this.findJob(chanJob, chanErr) // задания в очереди дженкинса не сразу появляются, по этому ждем их в отдельной горутине с таймаутом
	wg.Add(1)
	go func() {
		defer wg.Done()

		chanErr := make(chan error)
		wg1, wg2 := new(sync.WaitGroup), new(sync.WaitGroup)
		wg1.Add(1)
		go func() {
			defer wg1.Done()

			for err := range chanErr {
				this.errors = append(this.errors, fmt.Sprintf("Задание завершилось с ошибкой: %q", err.Error()))
			}
		}()

		// После того как задание будет найдено оно появится в канале, проверяем его статус (так же с таймаутом)
		for jobURL := range chanJob {
			logrus.WithField("url", jobURL).Debug("Запуск гоутины для отслеживания статуса")
			wg2.Add(1)
			go func() {
				defer func() {
					logrus.WithField("url", jobURL).Debug("Завершение гоутины отслеживания статуса")
					wg2.Done()
				}()
				this.checkJobStatus(jobURL + "/api/xml", chanErr)
			}()
		}

		wg2.Wait()
		close(chanErr)
		wg1.Wait()
	}()

	for err := range chanErr {
		if err == errTimeOut {
			FTimeOut()
			return
		} else {
			FEror(err.Error())
		}
		return
	}

	wg.Wait()
	if len(this.errors) == 0 {
		FSuccess()
	} else {
		FEror(strings.Join(this.errors, "\n"))
	}
}

func (this *Jenkins) findJob(chanJob chan string, errChan chan error) {
	logrus.Debug("Поиск задаия")
	defer func() {
		close(errChan)
		close(chanJob)
	}()

	f := func() bool {
		url := this.RootURL + "/job/" + this.jobName + "/api/xml"
		netU := new(n.NetUtility).Construct(url, this.User, this.Pass)
		logrus.WithField("url", url).Debug("Плучеие xml")

		if result, err := netU.CallHTTP(http.MethodGet, time.Minute); err == nil {
			xmlroot, xmlerr := xmlpath.Parse(strings.NewReader(result))
			if xmlerr != nil {
				logrus.WithField("URL", url).Errorf("Ошибка чтения xml %q", xmlerr.Error())
				errChan <- err
			}

			urlNamePath, _ := xmlpath.Compile("/workflowJob/build/url/text()")
			urlIter := urlNamePath.Iter(xmlroot)
			for urlIter.Next() {
				if joburl := urlIter.Node().String(); joburl != "" {
					if !this.containsGroup(joburl + "/api/xml") {
						continue
					}
					if _, ok := this.jobURLs[joburl]; ok {
						continue
					}
					chanJob <- joburl
					this.jobURLs[joburl] = true
					if this.jobCount == len(this.jobURLs) {
						logrus.Debug("Все задаия найдены")
						return true
					}
				}
			}
		} else {
			errChan <- err
		}

		return false
	}

	if err := runWithTimeout(time.Minute*15, f); err != nil {
		errChan <- err
	}
}