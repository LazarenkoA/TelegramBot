package telegram

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"strconv"
	"strings"
	"time"

	cf "github.com/LazarenkoA/TelegramBot/Configuration"

	"github.com/sirupsen/logrus"

	logrusRotate "github.com/LazarenkoA/LogrusRotate"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type EventWorkRep struct {
	BeforeBuild []func()
	AfterBuild  []func()
}

type WorkRep struct {
	BaseTask
	EventWorkRep

	//repName    string
	ChoseRep     *cf.Repository
	startVersion int
	endVersion   int
	reportFile   *cf.ConfCommonData
}

type RepositoryInfo struct {
	Version int
	Author  string
	Date    time.Time
	Comment string
}

type Repository struct {
	BinPath string
	//tmpDBPath string
}

func (W *WorkRep) ProcessChose(ChoseData string) {
	for _, rep := range Confs.RepositoryConf {
		if rep.Name == ChoseData {
			W.ChoseRep = rep
			break
		}
	}
	W.gotoByName("GetVersion")

	W.hookInResponse = func(update *tgbotapi.Update) bool {
		var version string
		var err error
		var words []string

		version = W.GetMessage().Text

		if version == "-1" {
			words = append(words, version)
		} else {
			words = strings.Split(version, "-")
		}
		if len(words) > 2 || len(words) < 1 {
			W.gotoByName("GetVersion", fmt.Sprintf("Введите диапазон версий. Вы ввели %q", version))
			W.DeleteMsg(update.Message.MessageID)
		} else {
			if W.startVersion, err = strconv.Atoi(strings.Trim(words[0], " ")); err != nil {
				W.DeleteMsg(update.Message.MessageID)
				W.gotoByName("GetVersion", fmt.Sprintf("Введите версию. Вы ввели %q", words[0]))
				return false
			}
			if len(words) == 2 {
				if W.endVersion, err = strconv.Atoi(strings.Trim(words[1], " ")); err != nil {
					W.DeleteMsg(update.Message.MessageID)
					W.gotoByName("GetVersion", fmt.Sprintf("Введите версию. Вы ввели %q"))
					return false
				}
			}
		}

		W.DeleteMsg(update.Message.MessageID)
		W.gotoByName("SaveReport", "", W.steps[0].(*step).Msg)
		go W.GetRepositoryReport()
		return true
	}
}

func (W *WorkRep) GetRepositoryReport() {
	defer func() {
		if err := recover(); err != nil {
			logrus.WithField("Имя репозитория", W.ChoseRep.Name).Errorf("Произошла ошибка при получении отчета: %v", err)
			msg := fmt.Sprintf("Произошла ошибка при получении отчета: %v", err)
			W.bot.Send(tgbotapi.NewMessage(W.ChatID, msg))
		}
		W.invokeEndTask(reflect.TypeOf(W).String())
	}()

	W.reportFile = new(cf.ConfCommonData)
	if W.reportFile.BinPath == "" {
		W.reportFile.BinPath = Confs.BinPath
	}
	if W.reportFile.OutDir == "" {
		W.reportFile.OutDir = Confs.OutDir
	}

	Report, err := W.reportFile.SaveReport(W.ChoseRep, W.startVersion, W.endVersion)
	if err != nil {
		logrusRotate.StandardLogger().WithError(err).Panic("Не удалось получить отчет по хранилищу конфигурации")
	}

	var res string
	parcedRep, err := W.reportFile.GetReport(Report)
	for i, rep := range parcedRep {
		res += rep.Comment + "\n"
		if i%10 == 0 && i > 0 { // отправляем по 10
			W.bot.Send(tgbotapi.NewMessage(W.ChatID, res))
			res = ""
		}
	}
	if res != "" {
		W.bot.Send(tgbotapi.NewMessage(W.ChatID, res))
	}
}

func (W *WorkRep) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	W.BaseTask.Initialise(bot, update, finish)
	W.EndTask[reflect.TypeOf(W).String()] = []func(){finish}

	firstStep := new(step).Construct("Выберите конфигурацию", "ChooseConf", W, ButtonCancel|ButtonBack, 3)
	for _, rep := range Confs.RepositoryConf {
		Name := rep.Name // Обязательно через переменную, нужно для замыкания
		firstStep.appendButton(rep.Alias, func() { W.ProcessChose(Name) })
	}
	firstStep.reverseButton()

	W.steps = []IStep{
		firstStep,
		new(step).Construct("Введите версии хранилища (например 280-285 или 280, -1)", "GetVersion", W, 0, 2),
		new(step).Construct("Получаю отчет по хранилищу", "SaveReport", W, 0, 2),
	}

	W.AppendDescription(W.name)
	return W
}

func (W *WorkRep) Start() {
	logrus.WithField("description", W.GetDescription()).Debug("Start")

	W.steps[W.currentStep].invoke(&W.BaseTask)
}

func (W *WorkRep) InfoWrapper(task ITask) {
	OutDir := Confs.OutDir
	if strings.Trim(OutDir, "") == "" {
		OutDir, _ = ioutil.TempDir("", "")
	}
	W.info = fmt.Sprintf("ℹ Команда получает историю изменений версий конфигурации.")
	W.BaseTask.InfoWrapper(task)
}

func (W *WorkRep) GetCallBack() map[string]func() {
	return W.callback
}
