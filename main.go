package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	logrusRotate "github.com/LazarenkoA/LogrusRotate"
	red "github.com/LazarenkoA/TelegramBot/Redis"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	session "github.com/LazarenkoA/TelegramBot/Confs"
	n "github.com/LazarenkoA/TelegramBot/Net"
	redis "github.com/LazarenkoA/TelegramBot/Redis"
	tel "github.com/LazarenkoA/TelegramBot/TelegramTasks"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"

	"github.com/sirupsen/logrus"
)

type ngrokAPI struct {
	Tunnels []*struct {
		PublicUrl string `json:"public_url"`
	} `json:"tunnels"`
}
type RotateConf struct {
}
type Hook struct {
}

func (h *Hook) Levels() []logrus.Level {
	return []logrus.Level{logrus.ErrorLevel, logrus.PanicLevel}
}
func (h *Hook) Fire(en *logrus.Entry) error {
	log.Println(en.Message)
	return nil
}

/* type settings struct {
	BinPath       string                          `json:"BinPath"`
	Extensions    *cf.ExtensionsSettings          `json:"Extensions"`
	Configuration *cf.ConfigurationCommonSettings `json:"Configuration"`
} */

var (
	pass     string
	LogLevel int
	help     bool
	//ngrokNotUse = errors.New("ngrok не используется")
	//handlers     map[string]tel.ITask
	//handlerMutex *sync.Mutex
)

func init() {
	//handlerMutex = new(sync.Mutex)
	flag.StringVar(&pass, "SetPass", "", "Установка нового пароля")
	flag.IntVar(&LogLevel, "LogLevel", 3, "Уровень логирования от 2 до 5, где 2 - ошибка, 3 - предупреждение, 4 - информация, 5 - дебаг")
	flag.BoolVar(&help, "help", false, "Помощь")
}

func main() {
	var err error

	flag.Parse()
	if help {
		flag.Usage()
		return
	}

	lw := new(logrusRotate.Rotate).Construct()
	defer lw.Start(LogLevel, new(RotateConf))()
	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.AddHook(new(Hook))

	fmt.Printf("%-50v", "Читаем настройки")
	Tasks := new(tel.Tasks)

	if err = Tasks.ReadSettings(); err == nil {
		fmt.Println("ОК")
	} else {
		fmt.Println("FAIL")
		logrus.Errorf("%v", err)
		return
	}
	fmt.Printf("%-50v", "Уровень логирования")
	fmt.Println(LogLevel)

	port := ListenAndServe()
	fmt.Printf("%-50v", "Слушаем порт")
	fmt.Println(port)

	tel.Confs.DIContainer.Provide(func() (*red.Redis, error) {
		return new(red.Redis).Create(tel.Confs.Redis)
	})

	fmt.Printf("%-50v", "Подключаемся к redis")
	err = tel.Confs.DIContainer.Invoke(func(r *red.Redis) {
		Tasks.SessManager = new(session.SessionManager).NewSessionManager(r)
	})
	if err == nil {
		fmt.Println("ОК")
	} else {
		fmt.Println("FAIL")
		logrus.WithError(err).Error("Ошибка получения redis объекта из контейнера")
	}

	if pass != "" {
		Tasks.SetPass(pass)
		fmt.Println("Пароль установлен")
		return
	}

	fmt.Printf("%-50v", "Получаем WebhookURL")
	var WebhookURL string
	if WebhookURL, err = getWebhookURL(); err == nil {
		fmt.Println("ОК")
	} else {
		fmt.Println("FAIL")
		logrus.Errorf("%v", err)
		return
	}

	fmt.Printf("%-50v", "Создаем бота")
	bot := NewBotAPI(WebhookURL)
	if bot == nil {
		logrus.Panic("Не удалось подключить бота")
		return
	}
	logrus.Debug("К боту подключились")
	fmt.Println("ОК")

	/* info, _ := bot.GetWebhookInfo()
	fmt.Println(info) */

	http.HandleFunc("/Debug", func(w http.ResponseWriter, r *http.Request) {
		//ioutil.ReadAll(r.Body)
		//defer r.Body.Close()
		if r.Method != http.MethodGet {
			http.Error(w, "Поддерживается только GET", http.StatusInternalServerError)
			return
		}

		fmt.Fprintln(w, "Конект есть")
	})
	http.HandleFunc("/setqueue", func(w http.ResponseWriter, r *http.Request) {
		logger := logrus.WithField("Handle", "setqueue")
		logger.Info()

		if r.Method != http.MethodPost {
			http.Error(w, "only the POST method is supported", http.StatusBadRequest)
			return
		}

		defer r.Body.Close()
		b, _ := ioutil.ReadAll(r.Body)
		//json := string(b)

		if len(b) == 0 {
			http.Error(w, "request body is empty", http.StatusBadRequest)
			return
		}

		logger.WithField("body", string(b)).Debug()

		data := map[string]interface{}{}
		if err := json.Unmarshal(b, &data); err != nil {
			logger.WithError(err).Error("ошибка десериализации json")
			http.Error(w, fmt.Errorf("json unmarshal error: %w", err).Error(), http.StatusInternalServerError)
			return
		}

		tel.Confs.DIContainer.Invoke(func(r *redis.Redis) {
			for k, v := range data {
				if msg, ok := v.(string); ok {
					logger.WithField("key", k).WithField("value", msg).Debug("add to queue redis")
					r.RPUSH(k, msg)
				} else {
					logger.WithField("data", data).Warning("bad data")
				}
			}
		})

	})

	updates := bot.ListenForWebhook("/")
	fmt.Println("Бот запущен.")
	mu := new(sync.Mutex) // некоторые задачи нельзя выполнять параллельно
	for _, t := range getHandler(mu) {
		go t.Daemon()
	}
	//tf := new(tel.TaskFactory)

	// получаем все обновления из канала updates
	for update := range updates {
		var Command string

		//isGroup := update.Message != nil && update.Message.Chat.IsGroup()
		//if isGroup {
		//	tf.Group().Initialise(bot, &update, func() {}).Start()
		//	continue
		//}
		//
		//if update.Message != nil && update.Message.Location != nil {
		//	fmt.Println(update.Message.Location.Latitude, update.Message.Location.Longitude)
		//}
		//if update.EditedMessage != nil && update.EditedMessage.Location != nil {
		//	fmt.Println("edit:", update.EditedMessage.Location.Latitude, update.EditedMessage.Location.Longitude)
		//}

		u := &update
		if !authorization(u, bot, Tasks) {
			continue
		}

		if update.CallbackQuery != nil {
			existNew := false
			for _, t := range Tasks.GetTasks(update.CallbackQuery.From.ID) {
				if t.GetState() != tel.StateDone {
					callback := t.GetCallBack()
					call := callback[update.CallbackQuery.Data]
					if call != nil {
						call()
					}
					existNew = true
				}
			}
			if !existNew {
				bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "Не найдено активных заданий."))
			}
			continue
		}

		if update.Message == nil {
			continue
		}

		Command = update.Message.Command()
		logrus.WithFields(logrus.Fields{
			"Command":   Command,
			"Msg":       update.Message.Text,
			"FirstName": update.Message.From.FirstName,
			"LastName":  update.Message.From.LastName,
			"UserName":  update.Message.From.UserName,
			"ChatID":    update.Message.Chat.ID,
		}).Debug()

		fromID := update.Message.From.ID
		// Чистим старые задания
		Tasks.Delete(fromID)

		var task tel.ITask
		switch Command {
		case "start":
			bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Привет %v %v!", update.Message.From.FirstName, update.Message.From.LastName)))
		case "cancel":
			//Tasks.Reset(fromID, bot, &update, true)
			//bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Готово!"))
		default:
			if task = Tasks.Reuse(Command, fromID); task != nil {
				break
			}

			if task = commandHandler(Command, mu); task != nil {
				Tasks.AppendTask(task, fromID)
			} else {
				// Проверяем общие хуки
				if Tasks.ExecuteHook(&update) {
					continue
				}

				// Сохренение файла. Обязательно асинхронно
				messageID := update.Message.MessageID
				message := update.Message
				go func() {
					var msg tgbotapi.MessageConfig
					if err := saveFile(message, bot); err != nil {
						msg = tgbotapi.NewMessage(message.Chat.ID, "Я такому необученный.")
						msg.ReplyToMessageID = messageID
					} else {
						msg = tgbotapi.NewMessage(message.Chat.ID, "👍🏻")
						msg.ReplyToMessageID = messageID
					}
					bot.Send(msg)
				}()
			}
		}
		if task != nil {
			// горутина нужна из-за Lock
			go func() {
				task.Lock(func() {
					txt := fmt.Sprintf("Kоманда %q является эксклюзивной (параллельно несколько аналогичных команд выполняться не могут). Дождитесь окончания работы предыдущей команды", task.GetName())
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, txt))
				})

				// if race.Enabled {
				// 	bot.Send(tgbotapi.NewMessage(task.GetChatID(), fmt.Sprintf("Коданда %q является эксклюзивной.\n Дождитесь завершения работы предыдущей команды", task.GetName())))
				// }
				task.InfoWrapper(task.Initialise(bot, &update, func() {
					task.SetState(tel.StateDone)
					msg := tgbotapi.NewMessage(task.GetChatID(), fmt.Sprintf("Задание:\n%v\nГотово!", task.GetDescription()))
					msg.ReplyToMessageID = task.CurrentStep().GetMessageID()
					bot.Send(msg)

					Tasks.Delete(fromID)
					task.Unlock()
				}))
			}()
		}
	}
}

func ListenAndServe() string {
	if net := tel.Confs.Network; net != nil {
		go http.ListenAndServe(":"+net.ListenPort, nil)
		//go http.ListenAndServeTLS(":"+net.ListenPort, "webhook_cert.pem", "webhook_pkey.key", nil) // для SSL
		logrus.Info("Слушаем порт " + net.ListenPort)
		return net.ListenPort
	} else {
		logrus.Panic("В настройках не определен параметр ListenPort")
	}
	return ""
}

func commandHandler(command string, mu *sync.Mutex) (task tel.ITask) {
	handlers := getHandler(mu)
	if task, ok := handlers[command]; ok {
		task.SetName(command)
		return task
	} else {
		return nil
	}
}

func getHandler(mu *sync.Mutex) map[string]tel.ITask {
	//handlerMutex.Lock()
	//defer handlerMutex.Unlock()

	tf := new(tel.TaskFactory)
	return map[string]tel.ITask{
		"buildcf":                 tf.BuildCf(),
		"buildcfe":                tf.BuildCfe(),
		"buildanduploadcf":        tf.BuilAndUploadCf(),
		"buildanduploadcfe":       tf.BuilAndUploadCfe(),
		"getlistupdatestate":      tf.GetListUpdateState(),
		"setplanupdate":           tf.SetPlanUpdate(),
		"invokeupdate":            tf.IvokeUpdate(),
		"deployextension":         tf.DeployExtension(mu),
		"invokeupdateactualcfe":   tf.InvokeUpdateActualCFE(),
		"disablezabbixmonitoring": tf.DisableZabbixMonitoring(),
		"charts":                  tf.Charts(),
		"sendmsg":                 tf.SendMsg(),
		"sui":                     tf.SUI(),
		"getrepositoryreport":     tf.GetRepositoryReport(),
	}
}

func authorization(update *tgbotapi.Update, bot *tgbotapi.BotAPI, Tasks *tel.Tasks) bool {
	if update.Message == nil {
		return true // вот такое допущение
	}

	User := update.Message.From
	var redis *red.Redis
	tel.Confs.DIContainer.Invoke(func(r *red.Redis) {
		redis = r
	})

	if (update.Message.Command() != "" && update.Message.Command() != "start") || update.Message.Text != "" {

		if ok := Tasks.CheckSession(User, update.Message.Text); !ok {
			if Tasks.CheckPass(User, update.Message.Text) {
				// сохраняем данные авторизованого пользователя
				if redis == nil {
					return false
				}

				redis.AppendItems("users", User.UserName)
				redis.SetMap(User.UserName, map[string]string{
					"UserName":  User.UserName,
					"FirstName": User.FirstName,
					"LastName":  User.LastName,
					"ChatID":    strconv.FormatInt(update.Message.Chat.ID, 10),
				})

				// Удаляем пароль
				bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
					ChatID:    update.Message.Chat.ID,
					MessageID: update.Message.MessageID,
				})

				// удаляем картинки
				for _, v := range redis.Items("imgMSG") {
					ChatIDstr, _ := redis.Get(v)
					ChatID, _ := strconv.ParseInt(ChatIDstr, 10, 64)
					MessageID, _ := strconv.Atoi(v)

					bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
						ChatID:    ChatID,
						MessageID: MessageID})

					redis.DeleteItems("imgMSG", v)
				}

				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "🧞‍♂ слушаюсь и повинуюсь."))
				return false // это спецом, что бы выше continue вызвался
			}

			currentDir, _ := os.Getwd()
			imgPath := filepath.Join(currentDir, "img", "notLogin.png")
			if _, err := os.Stat(imgPath); os.IsNotExist(err) {
				m, _ := bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Необходимо ввести пароль"))
				redis.Set(strconv.Itoa(m.MessageID), strconv.FormatInt(update.Message.Chat.ID, 10), 0)
				redis.AppendItems("imgMSG", strconv.Itoa(m.MessageID))
			} else {
				// для отправки файла NewDocumentUpload
				msg := tgbotapi.NewPhotoUpload(update.Message.Chat.ID, imgPath)
				quote := getQuote()
				if quote == "" {
					quote = "🤷🏻‍♂️ циату не придумал"
				}

				msg.Caption = quote
				msg.ParseMode = "HTML"
				m, _ := bot.Send(msg)
				redis.Set(strconv.Itoa(m.MessageID), strconv.FormatInt(update.Message.Chat.ID, 10), 0)
				redis.AppendItems("imgMSG", strconv.Itoa(m.MessageID))
			}
			return false
		}
	}

	return true
}

func getQuote() string {
	netU := new(n.NetUtility).Construct("https://api.forismatic.com/api/1.0/?method=getQuote&format=json&lang=ru", "", "")
	if response, err := netU.CallHTTP(http.MethodGet, time.Second*5, nil); err != nil {
		return ""
	} else {
		m := map[string]string{}
		if err := json.Unmarshal([]byte(response), &m); err != nil {
			return ""
		}
		return fmt.Sprintf("%q\n<i>%v</i>", m["quoteText"], m["quoteAuthor"])
	}
}

func getWebhookURL() (string, error) {
	if net := tel.Confs.Network; net != nil {
		if net.UseNgrok {
			// файл Ngrok должен лежать рядом с основным файлом бота
			currentDir, _ := os.Getwd()
			ngrokpath := filepath.Join(currentDir, "ngrok.exe")
			if _, err := os.Stat(ngrokpath); os.IsNotExist(err) {
				return "", fmt.Errorf("Файл ngrok.exe не найден")
			}

			err := make(chan error, 0)
			result := make(chan string, 0)

			// горутина для запуска ngrok
			go func(chanErr chan<- error) {
				cmd := exec.Command(ngrokpath, "http", net.ListenPort)
				err := cmd.Run()
				if err != nil {
					errText := fmt.Sprintf("Произошла ошибка запуска:\n err:%v \n", err.Error())

					if cmd.Stderr != nil {
						if stderr := cmd.Stderr.(*bytes.Buffer).String(); stderr != "" {
							errText += fmt.Sprintf("StdErr:%v", stderr)
						}
					}
					chanErr <- fmt.Errorf(errText)
					close(chanErr)
				}
			}(err)

			// горутина для получения адреса
			go func(result chan<- string, chanErr chan<- error) {
				// задумка такая, в горутине выше стартует Ngrok, после запуска поднимается вебсервер на порту 4040
				// и я могу получать url через api. Однако, в текущей горутине я не знаю стартанут там Ngrok или нет, по этому таймер
				// продуем подключиться 5 раз (5 сек) если не получилось, ошибка.
				tryCount := 5
				timer := time.NewTicker(time.Second * 1)
				for range timer.C {
					resp, err := http.Get("http://localhost:4040/api/tunnels")
					if (err == nil && !(resp.StatusCode >= http.StatusOK && resp.StatusCode <= http.StatusIMUsed)) || err != nil {
						if tryCount--; tryCount <= 0 {
							chanErr <- fmt.Errorf("Не удалось получить данные ngrok")
							close(chanErr)
							timer.Stop()
							return
						}
						continue
					}
					body, _ := ioutil.ReadAll(resp.Body)
					resp.Body.Close()

					var ngrok = new(ngrokAPI)
					err = json.Unmarshal(body, &ngrok)
					if err != nil {
						chanErr <- err
						close(chanErr)
						timer.Stop()
						return
					}
					if len(ngrok.Tunnels) == 0 {
						chanErr <- fmt.Errorf("Не удалось получить тунели ngrok")
						close(chanErr)
						timer.Stop()
						return
					}
					for _, url := range ngrok.Tunnels {
						if strings.Index(strings.ToLower(url.PublicUrl), "https") >= 0 {
							result <- url.PublicUrl
							close(result)
							timer.Stop()
							return
						}

					}
					chanErr <- fmt.Errorf("Не нашли https тунель ngrok")
					close(chanErr)
				}
			}(result, err)

			select {
			case e := <-err:
				return "", e
			case r := <-result:
				return r, nil
			}
		} else if net.WebhookURL != "" {
			return net.WebhookURL, nil
		} else {
			return "", fmt.Errorf("В настройках не задан UseNgrok или WebhookURL")
		}
	}

	return "", fmt.Errorf("В настройках не определен блок Network")
}

func saveFile(message *tgbotapi.Message, bot *tgbotapi.BotAPI) (err error) {
	downloadFilebyID := func(FileID string) {
		var file tgbotapi.File
		if file, err = bot.GetFile(tgbotapi.FileConfig{FileID}); err == nil {
			_, fileName := path.Split(file.FilePath)

			netU := new(n.NetUtility).Construct(file.Link(tel.Confs.BotToken), "", "")
			netU.Conf = tel.Confs
			err = netU.DownloadFile(path.Join("InFiles", fileName))
		}
	}

	if message.Video != nil {
		downloadFilebyID(message.Video.FileID)
	} else if message.Photo != nil {
		photos := *message.Photo
		// Последний элемент массива самого хорошего качества, берем его
		downloadFilebyID(photos[len(photos)-1].FileID)
	} else if message.Audio != nil {
		downloadFilebyID(message.Audio.FileID)
	} else if message.Voice != nil {
		downloadFilebyID(message.Voice.FileID)
	} else if message.Document != nil {
		downloadFilebyID(message.Document.FileID)
	} else {
		return fmt.Errorf("Не поддерживаемый тип данных")
	}

	return err
}

func NewBotAPI(WebhookURL string) *tgbotapi.BotAPI {

	bot, err := tgbotapi.NewBotAPIWithClient(tel.Confs.BotToken, n.GetHttpClient(tel.Confs))
	bot.Debug = tel.Confs.Debug

	if err != nil {
		logrus.Errorf("Произошла ошибка при создании бота: %q", err)
		return nil
	}
	logrus.Debug("Устанавливаем хук на URL " + WebhookURL)

	if tel.Confs.ResetWebhook {
		bot.RemoveWebhook()
	}
	//_, err = bot.SetWebhook(tgbotapi.NewWebhookWithCert(net.WebhookURL, "webhook_cert.pem"))
	_, err = bot.SetWebhook(tgbotapi.NewWebhook(WebhookURL))
	if err != nil {
		logrus.Errorf("Произошла ошибка при установки веб хука для бота: %q", err)
		return nil
	}
	if bot.Debug {
		if info, err := bot.GetWebhookInfo(); err == nil {
			logrus.WithField("webhookInfo", info).Debug()
		}
	}

	return bot
}

///////////////// RotateConf ////////////////////////////////////////////////////
func (w *RotateConf) LogDir() string {
	currentDir, _ := os.Getwd()
	return filepath.Join(currentDir, "Logs")
}
func (w *RotateConf) FormatDir() string {
	return "02.01.2006"
}
func (w *RotateConf) FormatFile() string {
	return "15"
}
func (w *RotateConf) TTLLogs() int {
	return 12
}
func (w *RotateConf) TimeRotate() int {
	return 1
}

// ДЛЯ ПАПЫ
/*
buildcfe - Собрать файлы расширений *.cfe
buildcf - Собрать файл конфигурации *.cf
buildanduploadcf - Собрать конфигурацию и отправить в менеджер сервиса
buildanduploadcfe - Собрать Файлы расширений и обновить в менеджер сервиса
setplanupdate - Запланировать обновление
getlistupdatestate - Получить список запланированных обновлений конфигураций
getrepositoryreport - Получение отчета по хранилищу
invokeupdate - Запуск задания jenkins для принудительного старта обработчиков обновления
invokeupdateactualcfe - Запуск обновлений расширений через jenkins
//deployextension - Отправка файла в МС, инкремент версии в ветки Dev, отправка задания на обновление в jenkins
disablezabbixmonitoring - Отключение zabbix мониторинга
charts - Графики
sui - Работа с заявками (создать, закрыть)
//cancel - Отмена текущего действия
*/

// go build -o "bot.exe" -ldflags "-s -w"
