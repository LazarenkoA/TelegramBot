package telegram

import (
	conf "TelegramBot/Configuration"
	settings "TelegramBot/Confs"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	//. "1C/TelegramTasks/charts"

	"github.com/garyburd/redigo/redis"
	uuid "github.com/nu7hatch/gouuid"
	"github.com/sirupsen/logrus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

const (
	StateNew int = iota
	StateWork
	StateDone

	ButtonNext = 1 << iota
	ButtonBack
	ButtonCancel
)

var IsRun int32

type ITask interface {
	Initialise(*tgbotapi.BotAPI, *tgbotapi.Update, func()) ITask
	Start()
	InfoWrapper(ITask)
	GetCallBack() map[string]func()
	GetHook() func(*tgbotapi.Update) bool
	RestHook()
	GetName() string
	GetState() int
	SetState(int)
	GetChatID() int64
	GetUUID() *uuid.UUID
	SetUUID(*uuid.UUID)
	SetName(string)
	back()
	next(txt string)
	GetDescription() string
	GetMessage() *tgbotapi.Message
	IsExclusively() bool
	Lock(func())
	Unlock()
	CurrentStep() IStep
}

type BaseTask struct {
	BaseEvent

	name           string
	callback       map[string]func()
	key            string
	description    map[string]bool // map для уникальности
	bot            *tgbotapi.BotAPI
	update         *tgbotapi.Update
	hookInResponse func(*tgbotapi.Update) bool
	state          int
	UUID           *uuid.UUID
	info           string
	ChatID         int64
	steps          []IStep
	currentStep    int
	mu             *sync.Mutex
}

type Tasks struct {
	tasks       map[int][]ITask
	passHash    string
	timer       map[int]*time.Ticker
	SessManager *settings.SessionManager
}

type TaskFactory struct {
}

type Bases struct {
	Caption  string `json:"Caption"`
	Name     string `json:"Name"`
	UUID     string `json:"UUID"`
	UserName string `json:"UserName"`
	UserPass string `json:"UserPass"`
	Cluster  *struct {
		MainServer string `json:"MainServer"`
		RASServer  string `json:"RASServer"`
		RASPort    int    `json:"RASPort"`
	} `json:"Cluster"`
	URL string `json:"URL"`
}

type BaseEvent struct {
	EndTask map[string][]func()
}

type IStep interface {
	invoke(object *BaseTask)
	invokeWithChangeCaption(object *BaseTask, txt string)
	String() string
	appendButton(caption string, Invoke func()) *step
	setPreviousStep(int)
	getPreviousStep() int
	reverseButton() *step
	GetMessageID() int
}

type step struct {
	txt                                              string
	stepName, nivigation                             string
	Buttons                                          []map[string]interface{}
	exitButtonCancel, exitButtonNext, exitButtonBack bool
	BCount                                           int
	previousStep                                     int
	whengoing                                        func(IStep)
	Msg                                              *tgbotapi.Message
}

var (
	Confs *conf.CommonConf
)

//////////////////////// Tasks ////////////////////////

func (B *Tasks) ReadSettings() (err error) {
	B.tasks = make(map[int][]ITask, 0)
	B.timer = make(map[int]*time.Ticker, 0)
	var currentDir string

	currentDir, err = os.Getwd()
	CommonConfPath := filepath.Join(currentDir, "Confs", "Common.conf")

	Confs = new(conf.CommonConf)
	return settings.ReadSettings(CommonConfPath, Confs)
}
func (B *Tasks) GetPss() string {
	if B.passHash != "" {
		return B.passHash
	}

	currentDir, _ := os.Getwd()
	CommonConfPath := filepath.Join(currentDir, "Confs", "pass")

	if _, err := os.Stat(CommonConfPath); os.IsNotExist(err) {
		logrus.WithField("файл", CommonConfPath).Panic("Файл с паролем не найден. Воспользуйтесь ключем запуска SetPass")
		return ""
	}

	file, err := ioutil.ReadFile(CommonConfPath)
	if err != nil {
		logrus.WithField("файл", CommonConfPath).WithField("Ошибка", err).Panic("Ошибка открытия файла")
		return ""
	}

	B.passHash = string(file)
	return B.passHash
}
func (B *Tasks) SetPass(pass string) error {
	B.passHash = GetHash(pass)

	currentDir, _ := os.Getwd()
	CommonConfPath := filepath.Join(currentDir, "Confs", "pass")
	err := ioutil.WriteFile(CommonConfPath, []byte(B.passHash), os.ModeExclusive)
	if err != nil {
		logrus.WithField("файл", CommonConfPath).WithField("Ошибка", err).Panic("Ошибка записи файла")
		return err
	}

	return nil
}
func (B *Tasks) CheckSession(User *tgbotapi.User, pass string) (bool, string) {
	//logrus.Debug("Авторизация")

	if B.SessManager == nil {
		return false, "Не задан менеджер сессии"
	}

	if passCash, err := B.SessManager.GetSessionData(User.ID); err == nil {
		if passCash == B.GetPss() {
			return true, ""
		} else {
			B.SessManager.DeleteSessionData(User.ID)
			return false, "В кеше не верный пароль"
		}
	} else if err == redis.ErrNil {
		// в кеше нет данных
		logrus.WithFields(logrus.Fields{
			"Пользователь": User.UserName,
			"Имя":          User.FirstName,
			"Фамилия":      User.LastName,
		}).Info("Попытка авторизации")

		if GetHash(pass) == B.GetPss() {
			if err := B.SessManager.AddSessionData(User.ID, GetHash(pass)); err != nil {
				return false, err.Error()
			}
			return true, "Пароль верный"
		}
	} else {
		return false, err.Error()
	}

	return false, "Пароль не верный"
}
func (B *Tasks) ExecuteHook(update *tgbotapi.Update) bool {
	result := false
	for _, t := range B.tasks[update.Message.From.ID] {
		if hook := t.GetHook(); hook != nil {
			result = true
			if hook(update) {
				t.RestHook()
			}
		}
	}

	return result
}
func (B *Tasks) AppendTask(task ITask, name string, UserID int, reUse bool) ITask {
	UUID, _ := uuid.NewV4()

	// Некоторые задания имеет смысл переиспользовать, например при получении списка заданий агента, что бы при повторном запросе видно было какие отслеживаются, а какие нет.
	if reUse {
		for _, t := range B.GetTasks(UserID) {
			if t.GetName() == name {
				return t
			}
		}
	}

	task.SetName(name)
	task.SetUUID(UUID)
	B.Append(task, UserID)

	return task
}
func (B *Tasks) Append(t ITask, UserID int) error {
	/* for _, item := range B.tasks[UserID] {
		if item.GetName() == t.GetName() && item.GetState() != StateDone {
			return fmt.Errorf("Задание %q уже выполняется", t.GetName())
		}
	} */
	B.tasks[UserID] = append(B.tasks[UserID], t)
	return nil
}
func (B *Tasks) Delete(UserID int) {
	for i := len(B.tasks[UserID]) - 1; i >= 0; i-- {
		if B.tasks[UserID][i].GetState() == StateDone {
			B.tasks[UserID] = append(B.tasks[UserID][:i], B.tasks[UserID][i+1:]...)
		}
	}
}
func (B *Tasks) GetTasks(UserID int) []ITask {
	return B.tasks[UserID]
}
func (B *Tasks) Reset(fromID int, bot *tgbotapi.BotAPI, update *tgbotapi.Update, clear bool) {
	if clear {
		B.clearTasks(fromID)
	}

	/* bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Вот что я умею:\nСобрать файл конфигурации cf /BuildCf\n\n"+
	"Собрать файлы расширений cfe /BuildCfe\n\n"+
	"Собрать конфигурацию и отправить во фреш /BuilAndUploadCf\n\n"+
	"Собрать Файлы расширений и обновить во фреше /BuilAndUploadCfe\n\n"+
	"Запланитьвать обновление /SetPlanUpdate\n\n"+
	"Получить список запланированных обновлений конфигураций /GetListUpdateState\n\n"+
	"Отмена текущего действия /Cancel")) */
}
func (B *Tasks) clearTasks(fromID int) {
	B.tasks[fromID] = make([]ITask, 0, 0)
}

//////////////////////// Common ////////////////////////

func GetHash(pass string) string {
	first := sha256.New()
	first.Write([]byte(pass))

	return fmt.Sprintf("%x", first.Sum(nil))
}

//////////////////////// Base struct ////////////////////////

func (B *BaseTask) CurrentStep() IStep  {
	return B.steps[B.currentStep]
}

func (B *BaseTask) Unlock() {
	if B.IsExclusively() {
		atomic.AddInt32(&IsRun, -1)
		B.mu.Unlock()
	}
}

func (B *BaseTask) Lock(busy func()) {
	if B.IsExclusively() {
		if IsRun > 0 && busy != nil {
			busy()
		}
		atomic.AddInt32(&IsRun, 1)
		B.mu.Lock()
	}
}

func (B *BaseTask) IsExclusively() bool {
	return B.mu != nil
}

func (B *BaseTask) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.bot = bot
	B.update = update

	// По дефолту ключ пустая строка, но бывают случаи когда нужно вызывать код для завершения таска не из родителя, а из потомка
	// например есть такое наследование классы A -> B -> C, т.е. A самый базовый остальные наследуются от него.
	// Когда из класса С вызывается процесс класса А (или B) нам не нужно что бы в А (или B) выполнился EndTask, он должен выполнится в С, для этого и сделано мапой, ключ будет имя класса
	B.EndTask = map[string][]func(){
		"": []func(){finish},
	}
	B.state = StateWork
	B.ChatID = B.GetMessage().Chat.ID
}

func (B *BaseTask) invokeEndTask(key string) {
	for _, f := range B.EndTask[key] {
		func() {
			logrus.WithField("task", B.GetDescription()).Debug("Завершили здание")
			f()
		}()
	}
}

func (B *BaseTask) Continue(task ITask) {
	task.Start()
}

func (B *BaseTask) InfoWrapper(task ITask) {
	if task == nil {
		return
	}
	Buttons := make([]map[string]interface{}, 0)
	B.appendButton(&Buttons, "✅ Продолжить", func() { B.Continue(task) })

	msg := tgbotapi.NewMessage(B.ChatID, B.info)
	B.createButtons(&msg, Buttons, 2, true)
	B.bot.Send(msg)
}
func (B *BaseTask) AppendDescription(txt string) {
	if B.description == nil {
		B.description = make(map[string]bool, 0)
	}

	B.description[txt] = true
}
func (B *BaseTask) GetDescription() (result string) {
	for v, _ := range B.description {
		result += v + "\n"
	}
	return result
}
func (B *BaseTask) Cancel() {
	B.state = StateDone
	B.bot.Send(tgbotapi.NewMessage(B.ChatID, "Задание отменено.\n"+B.GetDescription()))
	B.Unlock()
}
func (B *BaseTask) breakButtonsByColum(Buttons []tgbotapi.InlineKeyboardButton, countColum int) [][]tgbotapi.InlineKeyboardButton {
	end := 0
	result := [][]tgbotapi.InlineKeyboardButton{}

	for i := 1; i <= int(float64(len(Buttons)/countColum)); i++ {
		end = i * countColum
		start := (i - 1) * countColum
		if end > len(Buttons) {
			end = len(Buttons)
		}

		row := tgbotapi.NewInlineKeyboardRow(Buttons[start:end]...)
		result = append(result, row)
	}
	if len(Buttons)%countColum > 0 {
		row := tgbotapi.NewInlineKeyboardRow(Buttons[end:len(Buttons)]...)
		result = append(result, row)
	}

	return result
}
func (B *BaseTask) GetName() string {
	return B.name
}
func (B *BaseTask) GetUUID() *uuid.UUID {
	return B.UUID
}
func (B *BaseTask) GetKey() string {
	return B.key
}
func (B *BaseTask) GetHook() func(*tgbotapi.Update) bool {
	return B.hookInResponse
}
func (B *BaseTask) RestHook() {
	B.hookInResponse = nil
}
func (B *BaseTask) GetCallBack() map[string]func() {
	return B.callback
}

func (B *BaseTask) GetState() int {
	return B.state
}

func (B *BaseTask) GetChatID() int64 {
	return B.ChatID
}

func (B *BaseTask) SetState(newState int) {
	B.state = newState
}

func (B *BaseTask) JsonUnmarshal(JSON string, v interface{}) error {
	if JSON == "" {
		return fmt.Errorf("Сервис вернул пустой ответ")
	}

	err := json.Unmarshal([]byte(JSON), v)
	if err != nil {
		logrus.WithField("JSON", JSON).WithError(err).Error()
	}

	return err
}

func (B *BaseTask) GetMessage() *tgbotapi.Message {
	var Message *tgbotapi.Message

	if B.update.CallbackQuery != nil {
		Message = B.update.CallbackQuery.Message
	} else {
		Message = B.update.Message
	}

	logrus.WithField("CallbackQuery", B.update.CallbackQuery != nil).WithField("Message", Message).Debug()

	return Message
}
func (B *BaseTask) createButtons(Msg *tgbotapi.MessageConfig, data []map[string]interface{}, countColum int, addCancel bool) tgbotapi.InlineKeyboardMarkup {
	keyboard := tgbotapi.InlineKeyboardMarkup{}
	var Buttons = []tgbotapi.InlineKeyboardButton{}

	if B.callback == nil {
		B.callback = make(map[string]func(), 0)
	}
	for _, item := range data {
		ID := item["ID"].(string)
		if _, ok := B.callback[ID]; ok {
			continue // если с таким id значит что-то не так
		}

		btn := tgbotapi.NewInlineKeyboardButtonData(item["Caption"].(string), ID)
		B.callback[ID] = item["Invoke"].(func())
		Buttons = append(Buttons, btn)
	}

	if addCancel {
		UUID, _ := uuid.NewV4()
		Buttons = append(Buttons, tgbotapi.NewInlineKeyboardButtonData("🚫 Прервать", UUID.String()))
		B.callback[UUID.String()] = B.Cancel
	}

	keyboard.InlineKeyboard = B.breakButtonsByColum(Buttons, countColum)
	if Msg != nil {
		Msg.ReplyMarkup = &keyboard
	}

	return keyboard
}
func (B *BaseTask) appendButton(Buttons *[]map[string]interface{}, Caption string, Invoke func()) {
	UUID, _ := uuid.NewV4()
	*Buttons = append(*Buttons, map[string]interface{}{
		"Caption": Caption,
		"ID":      UUID.String(),
		"Invoke":  Invoke,
	})
}
func (B *BaseTask) SetUUID(UUID *uuid.UUID) {
	B.UUID = UUID
}
func (B *BaseTask) SetName(name string) {
	B.name = name
}
func (this *BaseTask) goTo(step int, txt string) {
	previousStep := this.currentStep
	if step > len(this.steps)-1 {
		step = len(this.steps) - 1
	}

	this.currentStep = step
	this.steps[this.currentStep].setPreviousStep(previousStep)
	if txt == "" {
		this.steps[step].invoke(this)
	} else {
		this.steps[step].invokeWithChangeCaption(this, txt)
	}

}
func (this *BaseTask) next(txt string) {
	this.currentStep++
	if this.currentStep > len(this.steps)-1 {
		this.currentStep = len(this.steps) - 1
	}

	this.steps[this.currentStep].setPreviousStep(this.currentStep - 1)
	if txt == "" {
		this.steps[this.currentStep].invoke(this)
	} else {
		this.steps[this.currentStep].invokeWithChangeCaption(this, txt)
	}
}
func (this *BaseTask) back() {
	// вернуться назад это не обязательно -1, потому что можно через goto или skip перейти к шагу, мы должны вернуться к предыдущему шагу
	previousStep := this.steps[this.currentStep].getPreviousStep()
	if previousStep < 0 {
		previousStep = 0
	}
	this.currentStep = previousStep
	this.steps[this.currentStep].invoke(this)
}

func (this *BaseTask) skipNext() {
	this.currentStep += 2
	if this.currentStep > len(this.steps)-1 {
		this.currentStep = len(this.steps) - 1
	}
	this.steps[this.currentStep].setPreviousStep(this.currentStep - 2)
	if this.currentStep > len(this.steps)-1 {
		this.currentStep = len(this.steps) - 1
	}

	this.steps[this.currentStep].invoke(this)
}

func (this *BaseTask) insertToFirst(step IStep) *BaseTask {
	tmp := make([]IStep, len(this.steps)+1)
	copy(tmp[1:], this.steps)
	tmp[0] = step
	this.steps = tmp

	return this
}

func (this *BaseTask) navigation() string {
	tmp := []string{}
	for _, st := range this.steps[:this.currentStep+1] {
		if step := fmt.Sprintf("%v", st); step != "" {
			tmp = append(tmp, fmt.Sprintf("[%v]", step))
		}
	}
	return strings.Join(tmp, " -> ")
}
func (this *BaseTask) DeleteMsg(MessageID int) {
	this.bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
		ChatID:    this.ChatID,
		MessageID: MessageID})
}

func (this *BaseTask) reverseSteps() {
	last := len(this.steps) - 1
	for i := 0; i < len(this.steps)/2; i++ {
		this.steps[i], this.steps[last-i] = this.steps[last-i], this.steps[i]
	}
}

//////////////////////// Task Factory ////////////////////////

func (this *TaskFactory) BuilAndUploadCf() ITask {
	return new(BuilAndUploadCf)
}
func (this *TaskFactory) BuilAndUploadCfe() ITask {
	return new(BuilAndUploadCfe)
}
func (this *TaskFactory) BuildCf() ITask {
	object := new(BuildCf)
	object.AllowSaveLastVersion = true // Флаг для того что бы можно было сохранять версию -1, т.е. актуальную (не всегда эо нужно)
	return object
}
func (this *TaskFactory) BuildCfe() ITask {
	return new(BuildCfe)
}
func (this *TaskFactory) DeployExtension(mu *sync.Mutex) ITask {
	object := new(DeployExtension)
	object.mu = mu
	return object
}
func (this *TaskFactory) GetListUpdateState() ITask {
	return new(GetListUpdateState)
}
func (this *TaskFactory) IvokeUpdate() ITask {
	return new(IvokeUpdate)
}
func (this *TaskFactory) SetPlanUpdate() ITask {
	return new(SetPlanUpdate)
}
func (this *TaskFactory) IvokeUpdateActualCFE() ITask {
	return new(IvokeUpdateActualCFE)
}

func (this *TaskFactory) DisableZabbixMonitoring() ITask {
	return new(DisableZabbixMonitoring)
}

func (this *TaskFactory) SendMsg() ITask {
	return new(SendMsg)
}

func (this *TaskFactory) Charts() ITask {
	return new(Charts)
}

//////////////////////// Step ////////////////////////

// Конструктор
//	BCount - Сколько кнопок в ряду
//	Buttons - Какие кнопки выводить
func (this *step) Construct(msg, name string, object ITask, Buttons, BCount int) *step {
	this.Buttons = []map[string]interface{}{}
	this.txt = msg
	this.stepName = name
	this.BCount = BCount

	this.addDefaultButtons(object, Buttons)

	return this
}

func (this *step) whenGoing(f func(IStep)) *step {
	this.whengoing = f
	return this
}

func (this *step) SetCaption(txt string) {
	this.txt = txt
}

func (this *step) addDefaultButtons(object ITask, Buttons int) {
	// Создаем стандартные кнопки навигации (вперед, назад)
	if this.exitButtonNext = Buttons&ButtonNext == ButtonNext; this.exitButtonNext {
		this.appendButton("➡️", func() { object.next("") })
	}
	if this.exitButtonBack = Buttons&ButtonBack == ButtonBack; this.exitButtonBack {
		this.appendButton("⬅️", object.back)
	}
	this.exitButtonCancel = Buttons&ButtonCancel == ButtonCancel
}

func (this *step) String() string {
	return this.nivigation
}

func (this *step) appendButton(caption string, Invoke func()) *step {
	UUID, _ := uuid.NewV4()
	newButton := map[string]interface{}{
		"Caption": caption,
		"ID":      UUID.String(),
		"Invoke": func() {
			this.nivigation = fmt.Sprintf("%v (%v)", this.stepName, caption)
			Invoke()
		},
	}

	// на тек. момент уже должны быть кнопки вперед и назад, добавляемые должны быть посередине
	if len(this.Buttons) > 0 {
		this.Buttons = append(this.Buttons[:1], append([]map[string]interface{}{newButton}, this.Buttons[1:]...)...)
	} else {
		this.Buttons = append(this.Buttons, newButton)
	}

	return this
}

func (this *step) reverseButton() *step {
	last := len(this.Buttons) - 1
	for i := 0; i < len(this.Buttons)/2; i++ {
		this.Buttons[i], this.Buttons[last-i] = this.Buttons[last-i], this.Buttons[i]
	}

	return this
}

func (this *step) invokeWithChangeCaption(object *BaseTask, txt string) {
	this.txt = txt
	this.invoke(object)

}

func (this *step) invoke(object *BaseTask) {
	//buttons := []map[string]interface{}{}
	// if object.currentStep == len(object.steps)-1 && this.exitButtonNext {
	// 	buttons = this.Buttons[:len(this.Buttons)-1]
	// } else if object.currentStep == 0 && this.exitButtonBack {
	// 	buttons = this.Buttons[1:]
	// } else {
	// 	buttons = this.Buttons
	// }

	if this.whengoing != nil {
		this.whengoing(this)
	}

	object.callback = nil // эт прям нужно
	if this.Msg == nil {
		this.Msg = object.GetMessage()
	}

	keyboardMarkup := object.createButtons(nil, this.Buttons, this.BCount, this.exitButtonCancel)
	text := this.txt + "\n\n<b>Навигация:</b>\n<i>" + object.navigation() + "</i>"

	msg := tgbotapi.NewEditMessageText(object.ChatID, this.Msg.MessageID, text)
	msg.ReplyMarkup = &keyboardMarkup
	msg.ParseMode = "HTML"
	object.bot.Send(msg)

}

func (this *step) setPreviousStep(step int) {
	this.previousStep = step
}

func (this *step) getPreviousStep() int {
	return this.previousStep
}

func (this *step) GetMessageID() int {
	return this.Msg.MessageID
}