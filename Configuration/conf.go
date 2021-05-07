package configuration

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding/charmap"

	"github.com/sirupsen/logrus"
	di "go.uber.org/dig"
	"golang.org/x/text/encoding"
	xmlpath "gopkg.in/xmlpath.v2"
)

type extNames []string

type Repository struct {
	Path          string `json:"Path" yaml:"Path"`
	Alias         string `json:"Alias" yaml:"Alias"`
	ConfFreshName string `json:"ConfFreshName" yaml:"ConfFreshName"`
	Name          string `json:"Name" yaml:"Name"`
	Login         string `json:"Login" yaml:"Login"`
	Pass          string `json:"Pass" yaml:"Pass"`
}

type IFreshAuth interface {
	GetLogin() string
	GetPass() string
	GetService(string) string
}

type Fresh struct {
	URL      string            `json:"URL" yaml:"URL"`
	Login    string            `json:"Login" yaml:"Login"`
	Pass     string            `json:"Pass" yaml:"Pass"`
	Services map[string]string `json:"Services" yaml:"Services"`
}

type FreshConf struct {
	Name  string `json:"Name" yaml:"Name"`
	Alias string `json:"Alias" yaml:"Alias"`
	SM    *Fresh `json:"SM" yaml:"SM"`
	SA    *Fresh `json:"SA" yaml:"SA"`
}

type CommonConf struct {
	BinPath        string        `json:"BinPath" yaml:"BinPath"`
	OutDir         string        `json:"OutDir" yaml:"OutDir"`
	GitRep         string        `json:"GitRep" yaml:"GitRep"`
	RepositoryConf []*Repository `json:"RepositoryConf" yaml:"RepositoryConf"`
	Extensions     *struct {
		ExtensionsDir string `json:"ExtensionsDir" yaml:"ExtensionsDir"`
	} `json:"Extensions" yaml:"Extensions"`
	FreshConf []*FreshConf `json:"FreshConf" yaml:"FreshConf"`
	Network   *struct {
		PROXY_ADDR string `json:"PROXY_ADDR" yaml:"PROXY_ADDR"`
		ListenPort string `json:"ListenPort" yaml:"ListenPort"`
		UseNgrok   bool   `json:"UseNgrok" yaml:"UseNgrok"`
		WebhookURL string `json:"WebhookURL" yaml:"WebhookURL"`
	} `json:"Network" yaml:"Network"`
	Jenkins *struct {
		URL       string `json:"URL" yaml:"URL"`
		Login     string `json:"Login" yaml:"Login"`
		Password  string `json:"Password" yaml:"Password"`
		UserToken string `json:"UserToken" yaml:"UserToken"`
	} `json:"Jenkins" yaml:"Jenkins"`
	Zabbix *struct {
		URL      string `json:"URL" yaml:"URL"`
		Login    string `json:"Login" yaml:"Login"`
		Password string `json:"Password" yaml:"Password"`
	} `json:"Zabbix" yaml:"Zabbix"`
	Charts *struct {
		Login    string            `json:"Login" yaml:"Login"`
		Password string            `json:"Password" yaml:"Password"`
		Services map[string]string `json:"Services" yaml:"Services"`
	} `json:"Charts" yaml:"Charts"`
	LogDir   string `json:"LogDir" yaml:"LogDir"`
	BotToken string `json:"BotToken" yaml:"BotToken"`
	Redis    string `json:"Redis" yaml:"Redis"`
	SUI      struct {
		URL  string `json:"URL" yaml:"URL"`
		User string `json:"User" yaml:"User"`
		Pass string `json:"Pass" yaml:"Pass"`
	} `json:"SUI" yaml:"SUI"`

	DIContainer *di.Container
}

type IConfiguration interface {
	IsExtension() bool
	GetName() string
	GetID() string
	GetFilesDir() string
	GetFile() string
	IncVersion() error
}

type Extension struct {
	Base              string `json:"Base" yaml:"Base"`
	Name              string `json:"Name" yaml:"Name"`
	Version           string `json:"Version" yaml:"Version"`
	filesDir          string
	file              string
	ConfigurationFile string
	GUID              string `json:"GUID" yaml:"GUID"`
	logger            *logrus.Entry
}

type ConfCommonData struct {
	BinPath    string
	OutDir     string
	Version    string
	extensions []IConfiguration
	logger     *logrus.Entry
}

type RepositoryInfo struct {
	Version int
	Author  string
	Date    time.Time
	Comment string
}

//////////////// ConfCommonData ///////////////////////

func (this *ConfCommonData) GetReport(report string) (result []*RepositoryInfo, err error) {

	// Двойные кавычки в комментарии мешают, по этому мы заменяем из на одинарные
	report = strings.Replace(report, "\"\"", "'", -1)

	tmpArray := [][]string{}
	reg := regexp.MustCompile(`[\{]"#","([^"]+)["][\}]`)
	matches := reg.FindAllStringSubmatch(report, -1)
	for _, s := range matches {
		if s[1] == "Версия:" {
			tmpArray = append(tmpArray, []string{})
		}

		if len(tmpArray) > 0 {
			tmpArray[len(tmpArray)-1] = append(tmpArray[len(tmpArray)-1], s[1])
		}
	}

	r := strings.NewReplacer("\r", "", "\n", " ")
	for _, array := range tmpArray {
		RepInfo := new(RepositoryInfo)
		for id, s := range array {
			switch s {
			case "Версия:":
				if version, err := strconv.Atoi(array[id+1]); err == nil {
					RepInfo.Version = version
				}
			case "Пользователь:":
				RepInfo.Author = array[id+1]
			case "Комментарий:":
				// Комментария может не быть, по этому вот такой костыльчик
				if array[id+1] != "Изменены:" {
					RepInfo.Comment = r.Replace(array[id+1])
				}
			case "Дата создания:":
				if t, err := time.Parse("02.01.2006", array[id+1]); err == nil {
					RepInfo.Date = t
				}
			case "Время создания:":
				if !RepInfo.Date.IsZero() {
					str := RepInfo.Date.Format("02.01.2006") + " " + array[id+1]
					if t, err := time.Parse("02.01.2006 15:04:05", str); err == nil {
						RepInfo.Date = t
					}
				}
			}
		}
		RepInfo.Comment = fmt.Sprintf("%q (версия %v)", RepInfo.Comment, RepInfo.Version)
		result = append(result, RepInfo)
	}

	return result, nil
}

func (conf *ConfCommonData) SaveReport(rep *Repository, versionStart int, versionFinish int) (result string, errOut error) {
	defer conf.logger.Info("Отчет по хранилищу сохранен")
	conf.logger.Info("Сохраняем отчет хранилища")

	defer func() {
		if err := recover(); err != nil {
			conf.logger.WithField("error", err).Error("Произошла ошибка при сохранении отчета")
		}
	}()

	fileLog := conf.createTmpFile()
	fileResult := conf.createTmpFile()
	//tmpCFDir, _ := ioutil.TempDir(conf.OutDir, "1c_Report_")
	var tmpDBPath string
	if tmpDBPath, errOut = conf.CreateTmpBD(); errOut != nil {
		conf.logger.Panicf("Не удалось создать временную базу, ошибка %v", errOut.Error()) // в defer перехват
	}

	defer func() {
		os.RemoveAll(tmpDBPath)
		os.Remove(fileLog)
		os.Remove(fileResult)
	}()

	param := []string{}
	param = append(param, "DESIGNER")
	param = append(param, "/DisableStartupDialogs")
	param = append(param, "/DisableStartupMessages")
	param = append(param, fmt.Sprintf("/F %v", tmpDBPath))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryF %v", strings.Trim(rep.Path+rep.Name, " ")))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryN %v", rep.Login))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryP %v", rep.Pass))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryReport %v", fileResult))
	if versionStart > 0 || versionStart == -1 {
		param = append(param, fmt.Sprintf("-NBegin %d", versionStart))
	}
	if versionFinish > 0 {
		param = append(param, fmt.Sprintf("-NEnd %d", versionFinish))
	}

	param = append(param, fmt.Sprintf("/OUT %v", fileLog))

	cmd := exec.Command(conf.BinPath, param...)
	if err := conf.run(cmd, fileLog); err != nil {
		conf.logger.Panic(err)
	}

	if bytes, err := ReadFile(fileResult, nil); err == nil {
		return string(*bytes), errOut
	} else {
		conf.logger.Errorf("Произошла ошибка при чтерии отчета: %v", err)
		return "", errOut
	}
}

func (conf *ConfCommonData) createTmpFile() string {

	fileLog, err := ioutil.TempFile("", "OutLog_")
	if err != nil {
		panic(fmt.Errorf("ошибка получения временого файла:\n %v", err))
	}
	defer fileLog.Close() // Закрываем иначе в него 1С не сможет записать

	return fileLog.Name()
}

func (conf *ConfCommonData) GetExtensions() []IConfiguration {
	return conf.extensions
}

// CreateTmpBD метод создает временную базу данных
func (conf *ConfCommonData) CreateTmpBD() (result string, err error) {
	fileLog := conf.createTmpFile()
	defer os.Remove(fileLog)

	tmpDBPath, _ := ioutil.TempDir("", "1c_DB_")
	cmd := exec.Command(conf.BinPath, "CREATEINFOBASE", fmt.Sprintf("File=%v", tmpDBPath), fmt.Sprintf("/OUT  %v", fileLog))
	err = conf.run(cmd, fileLog)

	return tmpDBPath, err
}

func (conf *ConfCommonData) ReadVervionFromConf(cfPath string) (err error) {
	defer func() {
		if err := recover(); err != nil {
			conf.logger.WithField("error", err).Error("произошла ошибка при получении версии конфигурации")
		} else {
			conf.logger.Info("Версия конфигарации успешно получена")
		}
	}()

	conf.logger.Debug("Распаковка конфигурации для получения версии")

	currentDir, _ := os.Getwd()
	unpackV8Path := filepath.Join(currentDir, "v8unpack.exe")
	if _, err := os.Stat(unpackV8Path); os.IsNotExist(err) {
		conf.logger.Warning("Получение версии из cf. В каталоге программы не найден файл v8unpack.exe")
		return err
	}

	//if _, err := os.Stat("zlib1.dll"); os.IsNotExist(err) {
	//	conf.logger.Warning("Получение версии из cf. В каталоге программы не найден файл zlib1.dll.")
	//	return err
	//}

	if _, err := os.Stat(cfPath); os.IsNotExist(err) {
		conf.logger.Warningf("Получение версии из cf. Не найден файл %v.", cfPath)
		return err
	}

	fileLog := conf.createTmpFile()
	defer os.Remove(fileLog)

	tmpDir, _ := ioutil.TempDir("", "1c_confFiles_")
	defer func() { go os.RemoveAll(tmpDir) }() // каталог большой, по этому удаляем горутиной
	conf.run(exec.Command(unpackV8Path, "-P", cfPath, tmpDir), fileLog)

	ReadVervion := func(body string) string {
		lines := strings.Split(body, "\n")
		if len(lines) > 14 {
			line := lines[14] // хз как по другому вычленить, считаем что версия всегда в 14 строке
			line = strings.Replace(line, line[:strings.Index(line, "}")+1], "", 1)
			parts := strings.Split(line, ",")
			if len(parts) > 7 {
				return strings.Trim(parts[7], "\"")
			}
		}

		conf.logger.WithField("body", body).Warning("Структура файла какая-то не такая ☺")
		fmt.Println(lines)

		return ""
	}

	if err, path := FindFiles(tmpDir, "root"); err == nil {
		conf.logger.WithField("file", path).Debug("Читаем файл")
		if buf, err := ReadFile(path, nil); err == nil {
			guid := strings.Split(string(*buf), ",") // должно быть такое содержимое "{2,4a54c225-8008-44cf-936d-958fddf9461d,}
			if len(guid) == 3 {
				err, filedata := FindFiles(tmpDir, guid[1])
				if err != nil {
					return err
				}

				//conf.run(exec.Command(unpackV8Path, "-I", filedata, filedataunpack), fileLog)
				b, _ := ReadFile(filedata, nil)
				conf.logger.Tracef("Читаем версию из: %q", string(*b))
				conf.Version = ReadVervion(string(*b))

				conf.logger.Debugf("Получена версия %v", conf.Version)
			} else {
				conf.logger.Errorf("Ошибка формата, исходная строка: %q", string(*buf))
			}
		} else {
			return err
		}
	} else {
		return err
	}

	return nil
}

func (conf *ConfCommonData) SaveConfiguration(rep *Repository, revision int) (result string, errOut error) {
	defer conf.logger.Info("Конфигурация сохранена")
	conf.logger.Info("Сохраняем конфигарацию")

	defer func() {
		if err := recover(); err != nil {
			conf.logger.WithField("error", err).Error("произошла ошибка при сохранении конфигурации")
		}
	}()

	fileLog := conf.createTmpFile()
	tmpCFDir, _ := ioutil.TempDir(conf.OutDir, "1c_CF_")
	var tmpDBPath string
	if tmpDBPath, errOut = conf.CreateTmpBD(); errOut != nil {
		conf.logger.Panicf("Не удалось создать временную базу, ошибка %v", errOut.Error()) // в defer перехват
	}

	defer func() {
		os.RemoveAll(tmpDBPath)
		os.Remove(fileLog)
	}()

	CfName := filepath.Join(tmpCFDir, fmt.Sprintf("%v_%v.cf", rep.Name, revision))

	param := []string{}
	param = append(param, "DESIGNER")
	param = append(param, "/DisableStartupDialogs")
	param = append(param, "/DisableStartupMessages")
	param = append(param, fmt.Sprintf("/F %v", tmpDBPath))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryF %v", strings.Trim(rep.Path+rep.Name, " ")))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryN %v", rep.Login))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryP %v", rep.Pass))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryDumpCfg %v", CfName))
	param = append(param, fmt.Sprintf("-v %v", revision))
	param = append(param, fmt.Sprintf("/OUT %v", fileLog))

	cmd := exec.Command(conf.BinPath, param...)
	errOut = conf.run(cmd, fileLog)

	return CfName, errOut
}

func (conf *ConfCommonData) BuildExtensions(chExt chan<- IConfiguration, chError chan<- error, extNames extNames, beforeBuild func(IConfiguration)) (errOut error) {
	conf.logger.WithField("extName", extNames).Info("Собираем расширение")
	defer conf.logger.WithField("extName", extNames).Info("Расширения собраны")

	defer close(chExt)
	defer close(chError)
	defer func() {
		if err := recover(); err != nil {
			conf.logger.WithField("error", err).Error("произошла ошибка при сохранении конфигурации")
		}
	}()

	gr := new(sync.WaitGroup)
	runBuild := func(ext IConfiguration) {
		defer gr.Done()
		defer func() {
			if err := recover(); err != nil {
				chError <- fmt.Errorf("произошла ошибка при сохранении расширения %q:\n %q", ext.GetName(), err)
			}
		}()

		if tmpDBPath, err := conf.CreateTmpBD(); err != nil {
			conf.logger.Panicf("Не удалось создать временную базу, ошибка %v", err.Error()) // в defer перехват
		} else {
			defer os.RemoveAll(tmpDBPath)

			if e := conf.loadConfigFromFiles(ext, tmpDBPath); e != nil {
				conf.logger.Panicf("Не удалось загрузить расширение из файлов, ошибка %v", e)
			}
			if e := conf.saveConfigToFile(ext, tmpDBPath); e != nil {
				// могут быть ложные ошибки, вроде сохраняется, но код возврата 1
				conf.logger.WithError(e).Warning("Ошибка при сохранении расширения в файл, ошибка")
			}
		}

		chExt <- ext
	}

	for _, ext := range conf.extensions {
		if extNames.Empty() || extNames.In(ext.GetName()) {
			gr.Add(1)

			beforeBuild(ext)
			go runBuild(ext)
		}
	}

	gr.Wait()

	return nil
}

func (conf *ConfCommonData) loadConfigFromFiles(ext IConfiguration, tmpDBPath string) error {
	conf.logger.Debug("Загружаем конфигурацию из файлов")

	fileLog := conf.createTmpFile()
	defer os.Remove(fileLog)

	param := []string{}
	param = append(param, "DESIGNER")
	param = append(param, "/DisableStartupMessages")
	param = append(param, fmt.Sprintf("/F %v", tmpDBPath))
	param = append(param, fmt.Sprintf("/LoadConfigFromFiles %v", ext.GetFilesDir()))
	if ext.IsExtension() {
		param = append(param, fmt.Sprintf("-Extension %q", ext.GetName()))
	}
	param = append(param, fmt.Sprintf("/OUT %v", fileLog))

	/* for _, v := range param {
		fmt.Println(v)
	} */

	cmd := exec.Command(conf.BinPath, param...)
	return conf.run(cmd, fileLog)
}

func (conf *ConfCommonData) saveConfigToFile(ext IConfiguration, tmpDBPath string) error {
	conf.logger.Debug("Сохраняем конфигурацию в файл")

	fileLog := conf.createTmpFile()
	defer os.Remove(fileLog)

	param := []string{}
	param = append(param, "DESIGNER")
	param = append(param, "/DisableStartupMessages")
	param = append(param, fmt.Sprintf("/F %v", tmpDBPath))
	param = append(param, fmt.Sprintf("/DumpCfg %v", ext.GetFile()))
	if ext.IsExtension() {
		param = append(param, fmt.Sprintf("-Extension %q", ext.GetName()))
	}
	param = append(param, fmt.Sprintf("/OUT %v", fileLog))
	cmd := exec.Command(conf.BinPath, param...)

	return conf.run(cmd, fileLog)
}

//InitExtensions - находит расширения в каталоге
func (conf *ConfCommonData) InitExtensions(rootDir, outDir string) {
	// Проверяем тек. каталог является расширением, если нет смотрим под каталоги
	Ext := new(Extension)
	if Ext.Create(rootDir) {
		Ext.file = filepath.Join(outDir, Ext.Name+".cfe")
		conf.extensions = append(conf.extensions, Ext)
	} else {
		subDir := getSubDir(rootDir)
		for _, dir := range subDir {
			Ext := new(Extension)
			if Ext.Create(dir) {
				Ext.file = filepath.Join(outDir, Ext.Name+".cfe")
				conf.extensions = append(conf.extensions, Ext)
			}
		}
	}

}

func (this *ConfCommonData) run(cmd *exec.Cmd, fileLog string) error {
	this.logger.WithField("Исполняемый файл", cmd.Path).
		WithField("Параметры", cmd.Args).
		Debug("Выполняется команда пакетного запуска")

	timeout := time.Hour * 2 // сохранение большой конфигурации может быть долгим, но вряд ли больше 2х часов
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)
	errch := make(chan error, 1)

	readErrFile := func() string {
		if buf, err := ReadFile(fileLog, charmap.Windows1251.NewDecoder()); err == nil {
			return string(*buf)
		} else {
			this.logger.Error(err)
			return ""
		}
	}

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("Произошла ошибка запуска:\n\terr:%v\n\tПараметры: %v\n\t", err.Error(), cmd.Args)
	}

	// запускаем в горутине т.к. наблюдалось что при выполнении команд в пакетном режиме может происходить зависон, нам нужен таймаут
	go func() {
		errch <- cmd.Wait()
	}()

	select {
	case <-time.After(timeout): // timeout
		return fmt.Errorf("Выполнение команды прервано по таймауту\n\tПараметры: %v\n\t", cmd.Args)
	case err := <-errch:
		if err != nil {
			stderr := cmd.Stderr.(*bytes.Buffer).String()
			errText := fmt.Sprintf("Произошла ошибка запуска:\n\terr:%v\n\tПараметры: %v\n\t", err.Error(), cmd.Args)
			if stderr != "" {
				errText += fmt.Sprintf("StdErr:%v\n", stderr)
			}

			this.logger.WithField("Исполняемый файл", cmd.Path).
				WithField("nOutErrFile", readErrFile()).
				Error(errText)

			return errors.New(errText)
		} else {
			return nil
		}
	}
}

func (this *ConfCommonData) New(Confs *CommonConf) *ConfCommonData {
	this.BinPath = Confs.BinPath
	this.OutDir, _ = ioutil.TempDir(Confs.OutDir, "Ext_")
	this.InitExtensions(Confs.Extensions.ExtensionsDir, this.OutDir)
	this.logger = logrus.WithField("name", "configuration")

	return this
}

//////////////// Extension ///////////////////////

// Create - Создание и инициализация структуры
func (this *Extension) Create(rootDir string) bool {
	this.logger = logrus.WithField("name", "extension")

	this.ConfigurationFile = path.Join(rootDir, "Configuration.xml")
	if _, err := os.Stat(this.ConfigurationFile); os.IsNotExist(err) {
		//this.logger.WithField("Файл", this.ConfigurationFile).Error("Файл не существует")
		return false
	}

	file, err := os.Open(this.ConfigurationFile)

	if err != nil {
		this.logger.WithField("Файл", this.ConfigurationFile).Errorf("Ошибка открытия файла %q", err)
		return false
	}

	defer file.Close()

	xmlroot, xmlerr := xmlpath.Parse(bufio.NewReader(file))
	if xmlerr != nil {
		this.logger.WithField("Файл", this.ConfigurationFile).Errorf("Ошибка чтения xml %q", xmlerr.Error())
		return false
	}

	path := xmlpath.MustCompile("MetaDataObject/Configuration/Properties/Version/text()")
	if value, ok := path.String(xmlroot); ok {
		this.Version = value
	}

	path = xmlpath.MustCompile("MetaDataObject/Configuration/Properties/Name/text()")
	if value, ok := path.String(xmlroot); ok {
		this.Name = value
	}

	this.filesDir = rootDir
	return true
}

func (this *Extension) IsExtension() bool {
	return true
}

func (this *Extension) GetName() string {
	return this.Name
}

func (this *Extension) GetID() string {
	return this.GUID
}

func (this *Extension) GetFilesDir() string {
	return this.filesDir
}

func (this *Extension) GetFile() string {
	return this.file
}

func (this *Extension) IncVersion() (err error) {
	this.logger.WithField("Extension", this.Name).Debugf("Предыдущая версия %v", this.Version)
	// Версия должна разделяться точкой, последний разряд будет инкрементироваться
	if parts := strings.Split(this.Version, "."); len(parts) > 0 {
		version := 0
		if version, err = strconv.Atoi(parts[len(parts)-1]); err == nil {
			version++
			this.Version = fmt.Sprintf("%v.%d", strings.Join(parts[:len(parts)-1], "."), version)
			this.logger.WithField("Extension", this.Name).Debugf("Новая версия %v", this.Version)
		} else {
			err = fmt.Errorf("расширение %q, последний разряд не является числом", this.GetName())
			this.logger.Error(err)
		}

		file, err := os.Open(this.ConfigurationFile)
		if err != nil {
			this.logger.WithField("Файл", this.ConfigurationFile).Errorf("Ошибка открытия файла: %q", err)
			return err
		}

		// Меняем версию, без парсинга, поменять значение одного узла прям проблема, а повторять структуру xml в классе ой как не хочется
		// Читаем файл
		stat, _ := file.Stat()
		buf := make([]byte, stat.Size())
		if _, err = file.Read(buf); err != nil {
			this.logger.WithField("Файл", this.ConfigurationFile).Errorf("Ошибка чтения файла: %q", err)
			return err
		}
		file.Close()
		os.Remove(this.ConfigurationFile)

		xml := string(buf)
		reg := regexp.MustCompile(`(?i)(?:<Version>(.+?)<\/Version>|<Version\/>)`)
		xml = reg.ReplaceAllString(xml, "<Version>"+this.Version+"</Version>")

		// сохраняем файл
		file, err = os.OpenFile(this.ConfigurationFile, os.O_CREATE, os.ModeExclusive)
		if err != nil {
			this.logger.WithField("Файл", this.ConfigurationFile).Errorf("Ошибка создания файла: %q", err)
			return err
		}
		defer file.Close()

		if _, err := file.WriteString(xml); err != nil {
			this.logger.WithField("Файл", this.ConfigurationFile).Errorf("Ошибка записи файла: %q", err)
			return err
		}

	} else {
		err = fmt.Errorf("расширение %q не верный формат", this.GetName())
		this.logger.Error(err)
	}

	return err
}

//////////////// Fresh ///////////////////////
func (f *Fresh) GetLogin() string {
	return f.Login
}
func (f *Fresh) GetPass() string {
	return f.Pass
}
func (f *Fresh) GetService(name string) string {
	if value, ok := f.Services[name]; ok {
		return value
	}

	logrus.Errorf("Не найден сервис %q", name)
	return ""
}

//////////////// extNames ///////////////////////
func (e extNames) In(value string) bool {
	for _, item := range e {
		if value == item {
			return true
		}
	}

	return false
}

func (e extNames) Empty() bool {
	return len(e) == 0
}

//////////////// Common ///////////////////////
func getSubDir(rootDir string) []string {
	var result []string
	f := func(path string, info os.FileInfo, err error) error {
		if info != nil && info.IsDir() {
			result = append(result, path)
		}

		return nil
	}

	filepath.Walk(rootDir, f)
	return result
}

func FindFiles(rootDir, fileName string) (error, string) {
	if _, err := os.Stat(rootDir); os.IsNotExist(err) {
		return err, ""
	}

	Files, _ := GetFiles(rootDir)

	for _, file := range Files {
		if _, f := filepath.Split(file); f == fileName {
			return nil, file
		}
	}

	return fmt.Errorf("файл %q не найден в каталоге %q", fileName, rootDir), ""
}

func GetFiles(dirPath string) ([]string, int64) {
	var result []string
	var size int64
	f := func(path string, info os.FileInfo, err error) error {
		if info.IsDir() || info.Size() == 0 {
			return nil
		}

		result = append(result, path)
		size += info.Size()

		return nil
	}

	filepath.Walk(dirPath, f)
	return result, size
}

func ReadFile(filePath string, decoder *encoding.Decoder) (*[]byte, error) {
	//dec := charmap.Windows1251.NewDecoder()

	if fileB, err := ioutil.ReadFile(filePath); err == nil {
		// Разные кодировки = разные длины символов.
		if decoder != nil {
			newBuf := make([]byte, len(fileB)*2)
			decoder.Transform(newBuf, fileB, false)

			return &newBuf, nil
		}
		return &fileB, nil
	} else {
		return nil, fmt.Errorf("ошибка открытия файла %q:\n %v", filePath, err)
	}
}
