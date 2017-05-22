package main

import (
	"fmt"
	"net/smtp"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"bitbucket.org/zombiezen/cardcpx/natsort"
	"github.com/jordan-wright/email"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
)

func main() {
	confPath := "/etc/reducemp4video"
	confFile := "reducemp4video"
	logFilename := "/var/log/reducemp4video/error.log"

	// confPath := "cfg/"
	// confFile := "reducemp4video_sample.toml"
	// logFilename := "error.log"

	loadConfig(&confPath, &confFile)

	fd := initLogging(&logFilename)
	defer fd.Close()

	if err := os.Chdir(viper.GetString("default.mp4folderpath")); err != nil {
		log := logging.MustGetLogger("log")

		log.Criticalf("Unable to chdir: %v", err)
		sendAnEmail(fmt.Sprintf("Unable to change directory: %v", err))

		os.Exit(1)
	}

	for {
		breakFilesListPtr := getFilesList("InProgress/*")
		filesListPtr := getFilesList("*.mp4")
		if len(*filesListPtr) != 0 {
			if len(*breakFilesListPtr) == 0 {
				createSomeFolders()
				splitMP4File(&(*filesListPtr)[0])
			}
			filesListToProcessePtr := getFilesList("InProgress/*.mkv")
			for _, filename := range *filesListToProcessePtr {
				transformToMKV(&filename)
			}
			sourceListPtr := getFilesList("InProgress/*.mp4")
			if len(*sourceListPtr) != 1 {
				// Souci en vu !
				sendAnEmail("J'ai un gros souci pour convertir les mkv. Je n'ai pas la chose attendu dans le répertoire \"InProgress\" !")
				os.Exit(1)
			}
			filesListToMergePtr := getFilesList("BeforeMerge/*.mkv")
			mergeMKV(sourceListPtr, filesListToMergePtr)

			for _, filename := range *sourceListPtr {
				os.Remove(filename)
			}
		}
		time.Sleep(time.Duration(viper.GetInt("default.sleeptime")) * time.Minute)
	}
}

func createSomeFolders() {
	os.Mkdir("InProgress", 0744)
	os.Mkdir("BeforeMerge", 0744)
	os.Mkdir("Finished", 0744)
}

func getFilesList(glob string) *[]string {
	log := logging.MustGetLogger("log")

	files, err := filepath.Glob(glob)
	if err != nil {
		log.Fatalf("Unable to get files list: %v", err)
		sendAnEmail(fmt.Sprintf("Unable to get files list with glob function: %v", err))
		os.Exit(1)
	}

	return &files
}

func mergeMKV(filenameListPtr *[]string, filesListToMergePtr *[]string) {
	natsort.Strings(*filesListToMergePtr)
	filename := strings.Replace((*filenameListPtr)[0], "InProgress", "Finished", -1)
	ext := path.Ext(filename)
	filename = fmt.Sprintf("%s.%s", filename[0:len(filename)-len(ext)], "mkv")
	cmd := fmt.Sprintf("mkvmerge -o %s %s", filename, strings.Join((*filesListToMergePtr), " + "))
	exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
	for _, filename := range *filesListToMergePtr {
		os.Remove(filename)
	}
}

func sendAnEmail(message string) {
	log := logging.MustGetLogger("log")

	host := viper.GetString("email.smtp")
	hostNPort := fmt.Sprintf("%s:%s", host, viper.GetString("email.port"))
	username := viper.GetString("email.login")
	password := viper.GetString("email.password")
	from := viper.GetString("email.from")
	to := viper.GetStringSlice("email.sendTo")

	e := email.NewEmail()
	e.From = from
	e.To = to
	e.Subject = "Problème détecté lors de la convertion de mkv"
	e.Text = []byte(message)
	if err := e.Send(hostNPort, smtp.PlainAuth("", username, password, host)); err != nil {
		log.Warningf("Unable to send an email to \"%s\": %v", strings.Join(to, " "), err)
	} else {
		log.Debugf("Email was sent to \"%s\"", strings.Join(to, " "))
	}
}

func splitMP4File(filename *string) {
	cmd := fmt.Sprintf("ffmpeg -i %s -acodec copy -f segment -segment_time 5 -vcodec copy -reset_timestamps 1 -map 0 InProgress/output%%d.mkv", *filename)
	exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
	os.OpenFile(fmt.Sprintf("InProgress/%s", *filename), os.O_RDONLY|os.O_CREATE, 0666)
	os.Remove(*filename)
}

func transformToMKV(filename *string) {
	newFilename := strings.Replace(*filename, "InProgress/", "BeforeMerge/", -1)
	cmd := fmt.Sprintf("ffmpeg -i %s -codec libx265 -crf 20 -preset veryslow -c:a copy %s", *filename, newFilename)
	exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
	os.Remove(*filename)
}
