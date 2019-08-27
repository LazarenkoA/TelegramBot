package telegram

import (
	conf "1C/Configuration"
	"1C/fresh"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type IvokeUpdateActualCFE struct {
	BuilAndUploadCfe
	DeployExtension
}

func (this *IvokeUpdateActualCFE) Initialise(bot *tgbotapi.BotAPI, update tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, &update, finish)

	this.fresh = new(fresh.Fresh)
	this.DeployExtension.Initialise(bot, update, finish) // так надо, особенность сложного наследования
	// у предка переопределяем события окончания выполнения, т.к. именно в методе предка конец
	this.DeployExtension.EndTask = []func(){}
	this.DeployExtension.EndTask = append(this.DeployExtension.EndTask, this.innerFinish)
	this.AppendDescription(this.name)

	return this
}

func (this *IvokeUpdateActualCFE) ChoseMC(ChoseData string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)
			this.baseFinishMsg(Msg)
		}
	}()

	for _, conffresh := range Confs.FreshConf {
		if ChoseData == conffresh.Name {
			this.fresh.Conf = conffresh
			break
		}
	}

	var extensions = []conf.Extension{}
	this.JsonUnmarshal(this.fresh.GetAllExtension(), &extensions)

	msg := tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Выберите расширение для установки")
	Buttons := make([]map[string]interface{}, 0, 0)

	for _, ext := range extensions {
		locExt := ext // Обязательно через переменную, нужно для замыкания
		this.appendButton(&Buttons, locExt.GetName(), func() { this.ChoseExt(&locExt) })
	}
	this.createButtons(&msg, Buttons, 3, true)
	this.bot.Send(msg)

}

func (this *IvokeUpdateActualCFE) ChoseExt(ext *conf.Extension) {

	// Вопрос как устанавливать, монопольно или нет
	msg := tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Отправляем задание в jenkins, установить монопольно?")
	this.callback = make(map[string]func())
	Buttons := make([]map[string]interface{}, 0)
	this.appendButton(&Buttons, "Да", func() {
		if err := this.InvokeJobJenkins(ext, true); err == nil {
			this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Задание отправлено в jenkins"))
		} else {
			this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
		}
	})
	this.appendButton(&Buttons, "Нет", func() {
		if err := this.InvokeJobJenkins(ext, false); err == nil {
			this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Задание отправлено в jenkins"))
		} else {
			this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
		}
	})

	this.createButtons(&msg, Buttons, 3, true)
	this.bot.Send(msg)
}

func (this *IvokeUpdateActualCFE) Start() {
	// 1. выбираем МС
	// 2. выбираем расширение

	// Для выбора МС вызываем Start предка (BuilAndUploadCfe)

	this.BuilAndUploadCfe.overriteChoseMC = this.ChoseMC
	this.BuilAndUploadCfe.Start()
}

func (this *IvokeUpdateActualCFE) innerFinish() {
	this.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", this.GetDescription()))
	this.outFinish()
}

func (B *IvokeUpdateActualCFE) InfoWrapper(task ITask) {
	B.info = "ℹ️ Команда инициирует обновления рсширений у выбранных баз через jenkins. " +
		"Задание в jenkins - update-cfe. Будет установлено актуальное на текущий момент расширение."
	B.BaseTask.InfoWrapper(task)
}
