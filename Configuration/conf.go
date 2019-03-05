package Configuration

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
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

type FreshSM struct {
	URL                        string `json:"URL"`
	RegExtensionServiceURL     string `json:"RegExtensionServiceURL"`
	UpLoadFileServiceURL       string `json:"UpLoadFileServiceURL"`
	RegConfigurationServiceURL string `json:"RegConfigurationServiceURL"`
	Login                      string `json:"Login"`
	Pass                       string `json:"Pass"`
	GetAvailableUpdates        string `json:"GetAvailableUpdates"`
	GetDatabase                string `json:"GetDatabase"`
	SetUpdetes                 string `json:"SetUpdetes"`
}
type FreshSA struct {
	URL                string `json:"URL"`
	GetListUpdateState string `json:"GetListUpdateState"`
	Login              string `json:"Login"`
	Pass               string `json:"Pass"`
}

type FreshConf struct {
	Name  string   `json:"Name"`
	Alias string   `json:"Alias"`
	SM    *FreshSM `json:"SM"`
	SA    *FreshSA `json:"SA"`
}

type CommonConf struct {
	BinPath        string        `json:"BinPath"`
	RepositoryConf []*Repository `json:"RepositoryConf"`
	Extensions     *Extensions   `json:"Extensions"`
	FreshConf      []*FreshConf  `json:"FreshConf"`
	Network        *Network      `json:"Network"`
	LogDir         string        `json:"LogDir"`
}

type Extensions struct {
	ExtensionsDir string `json:"ExtensionsDir"`
}

type Network struct {
	PROXY_ADDR string `json:"PROXY_ADDR"`
	ListenPort string `json:"ListenPort"`
	WebhookURL string `json:"WebhookURL"`
}

type IConfiguration interface {
	IsExtension() bool
	GetName() string
	GetFilesDir() string
	GetFile() string
}

type Extension struct {
	name     string
	Version  string
	filesDir string
	file     string
}

type ConfCommonData struct {
	BinPath    string
	extensions []IConfiguration
}

func (conf *ConfCommonData) createTmpFile() string {

	fileLog, err := ioutil.TempFile("", "OutLog_")
	defer fileLog.Close() // Закрываем иначе в него 1С не сможет записать

	if err != nil {
		panic(fmt.Errorf("Ошибка получения временого файла:\n %v", err))
	}

	return fileLog.Name()
}

func (conf *ConfCommonData) GetExtensions() []IConfiguration {
	return conf.extensions
}

// CreateTmpBD метод создает временную базу данных
func (conf *ConfCommonData) CreateTmpBD() string {
	fileLog := conf.createTmpFile()
	defer func() {
		os.Remove(fileLog)
	}()

	tmpDBPath, _ := ioutil.TempDir("", "1c_DB_")
	cmd := exec.Command(conf.BinPath, "CREATEINFOBASE", fmt.Sprintf("File=%v", tmpDBPath), fmt.Sprintf("/OUT  %v", fileLog))
	conf.run(cmd, fileLog)

	return tmpDBPath
}

func (conf *ConfCommonData) SaveConfiguration(rep *Repository, Revision int) (result string, errOut error) {
	defer logrus.Info("Конфигурация сохранена")
	logrus.Info("Сохраняем конфигарацию")

	defer func() {
		if err := recover(); err != nil {
			errOut = fmt.Errorf("Произошла ошибка при сохранении конфигурации: %q", err)
			logrus.Error(errOut)
		}
	}()

	fileLog := conf.createTmpFile()
	tmpCFDir, _ := ioutil.TempDir("", "1c_CF_")
	tmpDBPath := conf.CreateTmpBD()
	defer func() {
		os.RemoveAll(tmpDBPath)
		os.Remove(fileLog)
	}()

	CfName := filepath.Join(tmpCFDir, fmt.Sprintf("%v_%v.cf", rep.Name, Revision))

	param := []string{}
	param = append(param, "DESIGNER")
	param = append(param, "/DisableStartupDialogs")
	param = append(param, "/DisableStartupMessages")
	param = append(param, fmt.Sprintf("/F %v", tmpDBPath))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryF %v", strings.Trim(rep.Path, " ")))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryN %v", rep.Login))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryP %v", rep.Pass))
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryDumpCfg %v", CfName))
	param = append(param, fmt.Sprintf("/v %v", Revision))
	param = append(param, fmt.Sprintf("/OUT %v", fileLog))

	cmd := exec.Command(conf.BinPath, param...)
	conf.run(cmd, fileLog)

	return CfName, nil
}

func (conf *ConfCommonData) BuildExtensions(chExt chan<- string, chError chan<- error, ExtName string) (errOut error) {
	defer logrus.Info("Расширения собраны")
	defer logrus.Info("Собираем расширение")
	defer close(chExt)
	defer close(chError)

	defer func() {
		if err := recover(); err != nil {
			errOut = fmt.Errorf("Произошла ошибка при сохранении конфигурации: %q", err)
			logrus.Error(errOut)
		}
	}()

	gr := new(sync.WaitGroup)
	runBuild := func(ext IConfiguration) {
		defer gr.Done()
		defer func() {
			if err := recover(); err != nil {
				chError <- fmt.Errorf("Произошла ошибка при сохранении конфигурации: %q", err)
			}
		}()

		tmpDBPath := conf.CreateTmpBD()
		defer os.RemoveAll(tmpDBPath)

		conf.loadConfigFromFiles(ext, tmpDBPath)
		conf.saveConfigToFiles(ext, tmpDBPath)

		chExt <- ext.GetFile()
	}

	for _, ext := range conf.extensions {
		if ExtName == "" || ExtName == ext.GetName() {
			gr.Add(1)
			go runBuild(ext)
		}
	}

	gr.Wait()

	return nil
}

func (conf *ConfCommonData) loadConfigFromFiles(ext IConfiguration, tmpDBPath string) {
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
	conf.run(cmd, fileLog)
}

func (conf *ConfCommonData) saveConfigToFiles(ext IConfiguration, tmpDBPath string) {
	logrus.Debug("Сохраняем конфигурацию в файл")

	fileLog := conf.createTmpFile()
	defer func() {
		os.Remove(fileLog)
	}()

	param := []string{}
	param = append(param, "DESIGNER")
	param = append(param, "/DisableStartupMessages")
	param = append(param, fmt.Sprintf("/F %v", tmpDBPath))
	param = append(param, fmt.Sprintf("/DumpCfg %v", ext.GetFile()))
	if ext.IsExtension() {
		param = append(param, fmt.Sprintf("-Extension %q", ext.GetName()))
	}
	param = append(param, fmt.Sprintf("/OUT %v", fileLog))

	/* for _, v := range param {
		fmt.Println(v)
	} */

	cmd := exec.Command(conf.BinPath, param...)
	conf.run(cmd, fileLog)
}

//InitExtensions - находит расширения в каталоге
func (conf *ConfCommonData) InitExtensions(rootDir, outDir string) {
	// Проверяем тек. каталог является расширением, если нет смотрим под каталоги
	Ext := new(Extension)
	if Ext.Create(rootDir) {
		Ext.file = filepath.Join(outDir, Ext.name+".cfe")
		conf.extensions = append(conf.extensions, Ext)
	} else {
		subDir := getSubDir(rootDir)
		for _, dir := range subDir {
			Ext := new(Extension)
			if Ext.Create(dir) {
				Ext.file = filepath.Join(outDir, Ext.name+".cfe")
				conf.extensions = append(conf.extensions, Ext)
			}
		}
	}

}

func (conf *ConfCommonData) run(cmd *exec.Cmd, fileLog string) {
	logrus.WithField("Параметры", cmd.Args).Debug("Выполняется команда")

	//cmd.Stdin = strings.NewReader("some input")
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)

	readErrFile := func() string {
		dec := charmap.Windows1251.NewDecoder()

		if fileB, err := ioutil.ReadFile(fileLog); err == nil {
			// Разные кодировки = разные длины символов.
			newBuf := make([]byte, len(fileB)*2)
			dec.Transform(newBuf, fileB, false)

			return string(newBuf)
		} else {
			panic(fmt.Errorf("Ошибка открытия файла %q:\n %v", fileLog, err))
		}
	}

	err := cmd.Run()
	stderr := string(cmd.Stderr.(*bytes.Buffer).Bytes())
	if err != nil || stderr != "" {
		errText := fmt.Sprintf("Произошла ошибка запуска:\n StdErr:%q\n err:%q\n OutLog:%q", stderr, string(err.Error()), readErrFile())
		logrus.Error(errText)
		panic(errText)
	}

	/* print(string(Stdout.Bytes()))
	print(string(Stderr.Bytes())) */
}

//////////////// Extension ///////////////////////

// Create - Создание и инициализация структуры
func (Ex *Extension) Create(rootDir string) bool {
	Configuration := "/Configuration.xml"
	FilePath := rootDir + Configuration
	if _, err := os.Stat(FilePath); os.IsNotExist(err) {
		return false
	}

	file, err := os.Open(FilePath)
	defer file.Close()

	if err != nil {
		logrus.WithField("Файл", FilePath).Errorf("Ошибка открытия файла %q", err)
		return false
	}

	xmlroot, xmlerr := xmlpath.Parse(bufio.NewReader(file))
	if xmlerr != nil {
		logrus.WithField("Файл", FilePath).Errorf("Ошибка ошибка чтения xml %q", xmlerr.Error())
		return false
	}

	path := xmlpath.MustCompile("MetaDataObject/Configuration/Properties/Version/text()")
	if value, ok := path.String(xmlroot); ok {
		Ex.Version = value
	}

	path = xmlpath.MustCompile("MetaDataObject/Configuration/Properties/Name/text()")
	if value, ok := path.String(xmlroot); ok {
		Ex.name = value
	}

	Ex.filesDir = rootDir
	return true
}

func (Ex *Extension) IsExtension() bool {
	return true
}

func (Ex *Extension) GetName() string {
	return Ex.name
}

func (Ex *Extension) GetFilesDir() string {
	return Ex.filesDir
}

func (Ex *Extension) GetFile() string {
	return Ex.file
}

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
