package configuration

import (
	"bufio"
	"bytes"
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

	"github.com/sirupsen/logrus"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	xmlpath "gopkg.in/xmlpath.v2"
)

type Repository struct {
	Path          string `json:"Path"`
	Alias         string `json:"Alias"`
	ConfFreshName string `json:"ConfFreshName"`
	Name          string `json:"Name"`
	Login         string `json:"Login"`
	Pass          string `json:"Pass"`
}

type IFreshAuth interface {
	GetLogin() string
	GetPass() string
	GetService(string) string
}

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

type Fresh struct {
	URL      string            `json:"URL"`
	Login    string            `json:"Login"`
	Pass     string            `json:"Pass"`
	Services map[string]string `json:"Services"`
}

type FreshConf struct {
	Name  string `json:"Name"`
	Alias string `json:"Alias"`
	SM    *Fresh `json:"SM"`
	SA    *Fresh `json:"SA"`
}

type CommonConf struct {
	BinPath        string        `json:"BinPath"`
	OutDir         string        `json:"OutDir"`
	GitRep         string        `json:"GitRep"`
	RepositoryConf []*Repository `json:"RepositoryConf"`
	Extensions     *struct {
		ExtensionsDir string `json:"ExtensionsDir"`
	} `json:"Extensions"`
	FreshConf []*FreshConf `json:"FreshConf"`
	Network   *struct {
		PROXY_ADDR string `json:"PROXY_ADDR"`
		ListenPort string `json:"ListenPort"`
		UseNgrok   bool   `json:"UseNgrok"`
		WebhookURL string `json:"WebhookURL"`
	} `json:"Network"`
	Jenkins *struct {
		URL       string `json:"URL"`
		Login     string `json:"Login"`
		Password  string `json:"Password"`
		UserToken string `json:"UserToken"`
	} `json:"Jenkins"`
	Zabbix *struct {
		URL      string `json:"URL"`
		Login    string `json:"Login"`
		Password string `json:"Password"`
	} `json:"Zabbix"`
	Charts *struct {
		Login    string            `json:"Login"`
		Password string            `json:"Password"`
		Services map[string]string `json:"Services"`
	} `json:"Charts"`
	LogDir string `json:"LogDir"`
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
	Name              string `json:"Name"`
	Version           string `json:"Version"`
	filesDir          string
	file              string
	ConfigurationFile string
	GUID              string `json:"GUID"`
}

type ConfCommonData struct {
	BinPath    string
	OutDir     string
	Version    string
	extensions []IConfiguration
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
		if er := recover(); er != nil {
			err = fmt.Errorf("произошла ошибка при чтении версии из cf: %v", er)
			logrus.Error(err)
		}
	}()

	logrus.Debug("Распаковка конфигурации для получения версии")

	if _, err := os.Stat("unpackV8.exe"); os.IsNotExist(err) {
		logrus.Warning("Получение версии из cf. В каталоге программы не найден файл unpackV8.exe.")
		return err
	}

	if _, err := os.Stat("zlib1.dll"); os.IsNotExist(err) {
		logrus.Warning("Получение версии из cf. В каталоге программы не найден файл zlib1.dll.")
		return err
	}

	if _, err := os.Stat(cfPath); os.IsNotExist(err) {
		logrus.Warningf("Получение версии из cf. Не найден файл %v.", cfPath)
		return err
	}

	fileLog := conf.createTmpFile()
	defer os.Remove(fileLog)

	tmpDir, _ := ioutil.TempDir("", "1c_confFiles_")
	defer func() { go os.RemoveAll(tmpDir) }() // каталог большой, по этому удаляем горутиной

	currentDir, _ := os.Getwd()
	unpackV8Path := filepath.Join(currentDir, "unpackV8.exe")

	//param = append(param, "-parse") // parse не работает на конфе размером 900м хз почему
	conf.run(exec.Command(unpackV8Path, "-U", cfPath, tmpDir), fileLog)

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

		logrus.WithField("body", body).Warning("Структура файла какая-то не такая ☺")
		fmt.Println(lines)

		return ""
	}

	if err, path := FindFiles(tmpDir, "root.data"); err == nil {
		if buf, err := ReadFile(path, nil); err == nil {
			guid := strings.Split(string(*buf), ",") // должно быть такое содержимое "{2,4a54c225-8008-44cf-936d-958fddf9461d,}
			if len(guid) == 3 {
				_, filedata := FindFiles(tmpDir, guid[1]+".data")
				filedataunpack := conf.createTmpFile()
				defer os.Remove(filedataunpack)

				conf.run(exec.Command(unpackV8Path, "-I", filedata, filedataunpack), fileLog)
				b, _ := ReadFile(filedataunpack, nil)
				conf.Version = ReadVervion(string(*b))
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
	defer logrus.Info("Конфигурация сохранена")
	logrus.Info("Сохраняем конфигарацию")

	defer func() {
		if err := recover(); err != nil {
			errOut = fmt.Errorf("произошла ошибка при сохранении конфигурации: %q", err)
			logrus.Error(errOut)
		}
	}()

	fileLog := conf.createTmpFile()
	tmpCFDir, _ := ioutil.TempDir(conf.OutDir, "1c_CF_")
	var tmpDBPath string
	if tmpDBPath, errOut = conf.CreateTmpBD(); errOut != nil {
		logrus.Panicf("Не удалось создать временную базу, ошибка %v", errOut.Error()) // в defer перехват
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

func (conf *ConfCommonData) BuildExtensions(chExt chan<- IConfiguration, chError chan<- error, extName string, beforeBuild func(IConfiguration)) (errOut error) {
	logrus.Info("Собираем расширение")
	defer logrus.Info("Расширения собраны")
	defer close(chExt)
	defer close(chError)

	defer func() {
		if err := recover(); err != nil {
			logrus.Error(fmt.Errorf("произошла ошибка при сохранении конфигурации: %q", err))
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
			logrus.Panicf("Не удалось создать временную базу, ошибка %v", err.Error()) // в defer перехват
		} else {
			defer os.RemoveAll(tmpDBPath)

			if e := conf.loadConfigFromFiles(ext, tmpDBPath); e != nil {
				logrus.Panicf("Не удалось загрузить расширение из файлов, ошибка %v", e)
			}
			if e := conf.saveConfigToFile(ext, tmpDBPath); e != nil {
				// могут быть ложные ошибки, вроде сохраняется, но код возврата 1
				//logrus.Panicf("Не удалось сохранить расширение в файл, ошибка %v", e)
			}
		}

		chExt <- ext
	}

	for _, ext := range conf.extensions {
		if extName == "" || extName == ext.GetName() {
			gr.Add(1)

			beforeBuild(ext)
			go runBuild(ext)
		}
	}

	gr.Wait()

	return nil
}

func (conf *ConfCommonData) loadConfigFromFiles(ext IConfiguration, tmpDBPath string) error {
	logrus.Debug("Загружаем конфигурацию из файлов")

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
	logrus.Debug("Сохраняем конфигурацию в файл")

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

func (conf *ConfCommonData) run(cmd *exec.Cmd, fileLog string) error {
	logrus.WithField("Исполняемый файл", cmd.Path).
		WithField("Параметры", cmd.Args).
		Debug("Выполняется команда пакетного запуска")

	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)

	readErrFile := func() string {
		if buf, err := ReadFile(fileLog, charmap.Windows1251.NewDecoder()); err == nil {
			return string(*buf)
		} else {
			logrus.Error(err)
			return ""
		}
	}

	err := cmd.Run()
	stderr := cmd.Stderr.(*bytes.Buffer).String()
	if err != nil {
		errText := fmt.Sprintf("Произошла ошибка запуска:\n err:%v \n", err.Error())
		if stderr != "" {
			errText += fmt.Sprintf("StdErr:%v \n", stderr)
		}
		logrus.WithField("Исполняемый файл", cmd.Path).
			WithField("nOutErrFile", readErrFile()).
			Error(errText)
	}
	return err
}

func (this *ConfCommonData) New(Confs *CommonConf) *ConfCommonData {
	this.BinPath = Confs.BinPath
	this.OutDir, _ = ioutil.TempDir(Confs.OutDir, "Ext_")
	this.InitExtensions(Confs.Extensions.ExtensionsDir, this.OutDir)

	return this
}

//////////////// Extension ///////////////////////

// Create - Создание и инициализация структуры
func (this *Extension) Create(rootDir string) bool {
	this.ConfigurationFile = path.Join(rootDir, "Configuration.xml")
	if _, err := os.Stat(this.ConfigurationFile); os.IsNotExist(err) {
		//logrus.WithField("Файл", this.ConfigurationFile).Error("Файл не существует")
		return false
	}

	file, err := os.Open(this.ConfigurationFile)

	if err != nil {
		logrus.WithField("Файл", this.ConfigurationFile).Errorf("Ошибка открытия файла %q", err)
		return false
	}

	defer file.Close()

	xmlroot, xmlerr := xmlpath.Parse(bufio.NewReader(file))
	if xmlerr != nil {
		logrus.WithField("Файл", this.ConfigurationFile).Errorf("Ошибка чтения xml %q", xmlerr.Error())
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
	// Версия должна разделяться точкой, последний разряд будет инкрементироваться
	if parts := strings.Split(this.Version, "."); len(parts) > 0 {
		version := 0
		if version, err = strconv.Atoi(parts[len(parts)-1]); err == nil {
			version++
			this.Version = fmt.Sprintf("%v.%d", strings.Join(parts[:len(parts)-1], "."), version)
		} else {
			err = fmt.Errorf("расширение %q, последний разряд не является числом", this.GetName())
			logrus.Error(err)
		}

		file, err := os.Open(this.ConfigurationFile)
		if err != nil {
			logrus.WithField("Файл", this.ConfigurationFile).Errorf("Ошибка открытия файла: %q", err)
			return err
		}

		// Меняем версию, без парсинга, поменять значение одного узла прям проблема, а повторять структуру xml в классе ой как не хочется
		// Читаем файл
		stat, _ := file.Stat()
		buf := make([]byte, stat.Size())
		if _, err = file.Read(buf); err != nil {
			logrus.WithField("Файл", this.ConfigurationFile).Errorf("Ошибка чтения файла: %q", err)
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
			logrus.WithField("Файл", this.ConfigurationFile).Errorf("Ошибка создания файла: %q", err)
			return err
		}
		defer file.Close()

		if _, err := file.WriteString(xml); err != nil {
			logrus.WithField("Файл", this.ConfigurationFile).Errorf("Ошибка записи файла: %q", err)
			return err
		}

	} else {
		err = fmt.Errorf("расширение %q не верный формат", this.GetName())
		logrus.Error(err)
	}

	return err
}

// func (this *Extension) setVersion(newVersio string) (err error) {

// 	return nil
// }

//////////////// Common ///////////////////////
func getSubDir(rootDir string) []string {
	var result []string
	f := func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
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
