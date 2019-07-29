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

type FreshAuth interface {
	GetLogin() string
	GetPass() string
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

func (f *FreshSM) GetLogin() string {
	return f.Login
}
func (f *FreshSM) GetPass() string {
	return f.Pass
}

type FreshSA struct {
	URL                string `json:"URL"`
	GetListUpdateState string `json:"GetListUpdateState"`
	GeUpdateState      string `json:"GeUpdateState"`
	Login              string `json:"Login"`
	Pass               string `json:"Pass"`
}

func (f *FreshSA) GetLogin() string {
	return f.Login
}
func (f *FreshSA) GetPass() string {
	return f.Pass
}

type FreshConf struct {
	Name  string   `json:"Name"`
	Alias string   `json:"Alias"`
	SM    *FreshSM `json:"SM"`
	SA    *FreshSA `json:"SA"`
}

type Jenkins struct {
	URL      string `json:"URL"`
	Login    string `json:"Login"`
	Password string `json:"Password"`
}

type CommonConf struct {
	BinPath        string        `json:"BinPath"`
	OutDir         string        `json:"OutDir"`
	GitRep         string        `json:"GitRep"`
	RepositoryConf []*Repository `json:"RepositoryConf"`
	Extensions     *Extensions   `json:"Extensions"`
	FreshConf      []*FreshConf  `json:"FreshConf"`
	Network        *Network      `json:"Network"`
	Jenkins        *Jenkins      `json:"Jenkins"`
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
	OutDir     string
	Version    string
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
	defer os.Remove(fileLog)

	tmpDBPath, _ := ioutil.TempDir("", "1c_DB_")
	cmd := exec.Command(conf.BinPath, "CREATEINFOBASE", fmt.Sprintf("File=%v", tmpDBPath), fmt.Sprintf("/OUT  %v", fileLog))
	conf.run(cmd, fileLog)

	return tmpDBPath
}

func (conf *ConfCommonData) ReadVervionFromConf(CFPath string) (err error) {
	defer func() {
		if er := recover(); er != nil {
			err = fmt.Errorf("Произошла ошибка при чтении версии из cf: %v", er)
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

	if _, err := os.Stat(CFPath); os.IsNotExist(err) {
		logrus.Warningf("Получение версии из cf. Не найден файл %v.", CFPath)
		return err
	}

	fileLog := conf.createTmpFile()
	defer os.Remove(fileLog)

	tmpDir, _ := ioutil.TempDir("", "1c_confFiles_")
	defer func() { go os.RemoveAll(tmpDir) }() // каталог большой, по этому удаляем горутиной

	currentDir, _ := os.Getwd()
	unpackV8Path := filepath.Join(currentDir, "unpackV8.exe")

	//param = append(param, "-parse") // parse не работает на конфе размером 900м хз почему
	conf.run(exec.Command(unpackV8Path, "-U", CFPath, tmpDir), fileLog)

	ReadVervion := func(body string) string {
		lines := strings.Split(body, "\n")
		if len(lines) > 14 {
			line := lines[14] // хз как по другому вычленить, считаем что версия всегда в 14 строке
			line = strings.Replace(line, line[:strings.Index(line, "}")+1], "", 1)
			parts := strings.Split(line, ",")
			if len(parts) > 7 {
				return strings.Trim(parts[7], "\"")
			} else {
				logrus.WithField("body", body).Warning("Структура файла какая-то не такая ☺")
			}
		} else {
			logrus.WithField("body", body).Warning("Структура файла какая-то не такая ☺")
		}

		fmt.Println(lines)

		return ""
	}

	if err, path := FindFiles(tmpDir, "root.data"); err == nil {
		if err, buf := ReadFile(path, nil); err == nil {
			guid := strings.Split(string(*buf), ",") // должно быть такое содержимое "{2,4a54c225-8008-44cf-936d-958fddf9461d,}
			if len(guid) == 3 {
				_, filedata := FindFiles(tmpDir, guid[1]+".data")
				filedataunpack := conf.createTmpFile()
				defer os.Remove(filedataunpack)

				conf.run(exec.Command(unpackV8Path, "-I", filedata, filedataunpack), fileLog)
				_, b := ReadFile(filedataunpack, nil)
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
	tmpCFDir, _ := ioutil.TempDir(conf.OutDir, "1c_CF_")
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
	param = append(param, fmt.Sprintf("/ConfigurationRepositoryF %v", strings.Trim(rep.Path+rep.Name, " ")))
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
			logrus.Error(fmt.Errorf("Произошла ошибка при сохранении конфигурации: %q", err))
		}
	}()

	gr := new(sync.WaitGroup)
	runBuild := func(ext IConfiguration) {
		defer gr.Done()
		defer func() {
			if err := recover(); err != nil {
				chError <- fmt.Errorf("Произошла ошибка при сохранении расширения %q:\n %q", ext.GetName(), err)
			}
		}()

		tmpDBPath := conf.CreateTmpBD()
		defer os.RemoveAll(tmpDBPath)

		conf.loadConfigFromFiles(ext, tmpDBPath)
		conf.saveConfigToFile(ext, tmpDBPath)

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

func (conf *ConfCommonData) saveConfigToFile(ext IConfiguration, tmpDBPath string) {
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
	logrus.WithField("Исполняемый файл", cmd.Path).WithField("Параметры", cmd.Args).Debug("Выполняется команда пакетного запуска")

	//cmd.Stdin = strings.NewReader("some input")
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)

	readErrFile := func() string {
		if err, buf := ReadFile(fileLog, charmap.Windows1251.NewDecoder()); err == nil {
			return string(*buf)
		} else {
			logrus.Error(err)
			return ""
		}
	}

	err := cmd.Run()
	stderr := string(cmd.Stderr.(*bytes.Buffer).Bytes())
	if err != nil {
		errText := fmt.Sprintf("Произошла ошибка запуска:\nerr: %q \nOutErrFile: %q", string(err.Error()), readErrFile())
		logrus.Panic(errText)
	}
	if stderr != "" {
		errText := fmt.Sprintf("Произошла ошибка запуска:\nStdErr: %q \nOutErrFile: %q", stderr, readErrFile())
		logrus.Panic(errText)
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

	return fmt.Errorf("Файл %q не найден в каталоге %q", fileName, rootDir), ""
}

func GetFiles(DirPath string) ([]string, int64) {
	var result []string
	var size int64
	f := func(path string, info os.FileInfo, err error) error {
		if info.IsDir() || info.Size() == 0 {
			return nil
		} else {
			result = append(result, path)
			size += info.Size()
		}

		return nil
	}

	filepath.Walk(DirPath, f)
	return result, size
}

func ReadFile(filePath string, Decoder *encoding.Decoder) (error, *[]byte) {
	//dec := charmap.Windows1251.NewDecoder()

	if fileB, err := ioutil.ReadFile(filePath); err == nil {
		// Разные кодировки = разные длины символов.
		if Decoder != nil {
			newBuf := make([]byte, len(fileB)*2)
			Decoder.Transform(newBuf, fileB, false)

			return nil, &newBuf
		} else {
			return nil, &fileB
		}
	} else {
		return fmt.Errorf("Ошибка открытия файла %q:\n %v", filePath, err), nil
	}
}
