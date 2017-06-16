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

type myinfo struct {
	filename string
	size     int64
}

const (
	inProgessDir   = "InProgress/"
	beforeMergeDir = "BeforeMerge/"
	finishedDir    = "Finished/"
)

func main() {
	confPath := "/etc/reducemp4video"
	confFilename := "reducemp4video"
	logFilename := "/var/log/reducemp4video/error.log"

	// confPath := "cfg/"
	// confFilename := "reducemp4video_sample"
	// logFilename := "error.log"

	fd := initLogging(&logFilename)
	defer fd.Close()

	loadConfig(&confPath, &confFilename)

	if err := os.Chdir(viper.GetString("default.mp4folderpath")); err != nil {
		log := logging.MustGetLogger("log")

		log.Criticalf("Unable to chdir: %v", err)
		sendAnEmail(fmt.Sprintf("Unable to change directory: %v", err), "")

		os.Exit(1)
	}

	oldFilesList := []myinfo{}

	for {
		// obtention des fichiers qui sont dans InProgress. Cela veut dire que le programme s'est arrêté avant d'avoir tout fini
		// breakFilesListPtr := getFilesList(fmt.Sprintf("%s/*", inProgessDir))
		breakFilesListPtr := getFilesList(path.Join(inProgessDir, "*"))
		if len(*breakFilesListPtr) != 0 && !isHaveMP4File(breakFilesListPtr) {
			for _, filename := range *breakFilesListPtr {
				os.Remove(filename)
			}
			continue
		}
		// Obtention des fichiers à traiter
		filesListPtr := getFilesList("*.mp4")
		// Si la liste des fichiers à traiter n'est pas vide
		if len(*filesListPtr) != 0 || len(*breakFilesListPtr) != 0 {
			// S'il n'y a pas de fichier dans le répertoire InProgress
			if len(*breakFilesListPtr) == 0 {
				// On vérifie que les fichiers ne changent pas de taille avant d'en choisir un à traiter
				if isFilesSizeChanged(filesListPtr, &oldFilesList) {
					time.Sleep(2 * time.Second)
					continue
				}

				// Création des répertoires de travail s'ils n'existent pas déjà
				createSomeFolders()
				// Découpage du fichier sélectionné
				splitMP4File(&(*filesListPtr)[0])
				os.Remove((*filesListPtr)[0])
			}
			// obtention de la liste des fichiers coupés
			// filesListToProcessePtr := getFilesList(fmt.Sprintf("%s/*.mkv", inProgessDir))
			filesListToProcessePtr := getFilesList(path.Join(inProgessDir, "*.mkv"))
			for _, filename := range *filesListToProcessePtr {
				// Transformation de tous les fichiers contenus dans la liste
				transformToMKV(&filename)
				os.Remove(filename)
			}
			// Obtention du fichier vide pour remettre le bon nom
			// sourceListPtr := getFilesList("InProgress/*.mp4")
			sourceListPtr := getFilesList(path.Join(inProgessDir, "*.mp4"))
			// Normalement il n'y a qu'un seul fichier autrement c'est qu'il y a un sacré problème
			if len(*sourceListPtr) != 1 {
				// Souci en vu !
				sendAnEmail("I've a big shit to convert mkv's files. I've something wrong in folder \"InProgress\" !", "")
				os.Exit(1)
			}
			// filesListToMergePtr := getFilesList("BeforeMerge/*.mkv")
			filesListToMergePtr := getFilesList(path.Join(beforeMergeDir, "*.mkv"))
			// Fusion des fichiers mkv
			mergeMKV(sourceListPtr, filesListToMergePtr)
			// Remove filename which use to know latest filename after merge files
			for _, filename := range *filesListToMergePtr {
				os.Remove(filename)
			}

			// Remove all mkv files
			for _, filename := range *sourceListPtr {
				os.Remove(filename)
			}

			sendAnEmail(fmt.Sprintf("I finish processing %s. There are %d file(s) to reduce", (*filesListPtr)[0], len(*filesListPtr)-1), "End of reduce mp4 file.")
			continue
		}

		time.Sleep(time.Duration(viper.GetInt("default.sleeptime")) * time.Second)
	}
}

func isHaveMP4File(breakFilesListPtr *[]string) bool {
	log := logging.MustGetLogger("log")

	log.Debug("I search if I found an mp4 file in order to know if split is correct")
	for _, filename := range *breakFilesListPtr {
		ext := path.Ext(filename)
		if ext == ".mp4" {
			return true
		}
	}

	return false
}

func isFilesSizeChanged(filesListPtr *[]string, oldFilesListPtr *[]myinfo) bool {
	log := logging.MustGetLogger("log")

	log.Debug("I found new files and i see if their size has changed")

	newFilesList := make([]myinfo, len(*filesListPtr))

	// Get size of file for all files
	for num, filename := range *filesListPtr {
		file, err := os.Open(filename)
		if err != nil {
			log.Criticalf("Unable to open file: %v", err)
			os.Exit(1)
		}
		defer file.Close()

		fi, err := file.Stat()
		if err != nil {
			log.Critical("Unable to get file stat: %v", err)
			os.Exit(1)
		}

		newFilesList[num] = myinfo{filename: filename, size: fi.Size()}
	}

	if len(*oldFilesListPtr) == 0 {
		*oldFilesListPtr = newFilesList

		return true
	}

	for _, file := range newFilesList {
		for _, fi := range *oldFilesListPtr {
			if file.filename == fi.filename {
				if file.size != fi.size {
					log.Debug("size are differents")
					*oldFilesListPtr = newFilesList

					return true
				}
				break
			}
		}
	}

	return false
}

func createSomeFolders() {
	log := logging.MustGetLogger("log")

	log.Debug("I build folder to work")

	os.Mkdir(inProgessDir, 0744)
	os.Mkdir(beforeMergeDir, 0744)
	os.Mkdir(finishedDir, 0744)
}

func getFilesList(glob string) *[]string {
	log := logging.MustGetLogger("log")

	log.Debug("I'm searching some files")

	files, err := filepath.Glob(glob)
	if err != nil {
		log.Fatalf("Unable to get files list: %v", err)
		sendAnEmail(fmt.Sprintf("Unable to get files list with glob function: %v", err), "")
		os.Exit(1)
	}

	return &files
}

func findFilename(filesListPtr *[]string, filenameWithoutExt *string) *string {
	log := logging.MustGetLogger("log")

	tmpFilename := fmt.Sprintf("%s.%s", *filenameWithoutExt, "mkv")
	if searchStringInList(filesListPtr, &tmpFilename) {
		findNewName := false

		for i := 0; i < 1000; i++ {
			tmpFilename = fmt.Sprintf("%s-%d.%s", *filenameWithoutExt, i, "mkv")
			if !searchStringInList(filesListPtr, &tmpFilename) {
				findNewName = true
				break
			}
		}
		if !findNewName {
			log.Criticalf("Unable to find a filename for %s.mkv !", *filenameWithoutExt)
			os.Exit(0)
		}
	}

	return &tmpFilename
}

func mergeMKV(filenameListPtr *[]string, filesListToMergePtr *[]string) {
	log := logging.MustGetLogger("log")

	log.Debug("I merge some files")

	natsort.Strings(*filesListToMergePtr)
	filename := strings.Replace((*filenameListPtr)[0], inProgessDir, finishedDir, -1)
	ext := path.Ext(filename)
	filename = filename[0 : len(filename)-len(ext)]

	// filesListPtr := getFilesList("Finished/*.mkv")
	filesListPtr := getFilesList(path.Join(finishedDir, "*.mkv"))
	lastFilename := findFilename(filesListPtr, &filename)

	cmd := fmt.Sprintf("mkvmerge -o %s %s", *lastFilename, strings.Join((*filesListToMergePtr), " + "))
	exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
}

// Search if filename already exist in file's list
func searchStringInList(stringList *[]string, str *string) bool {
	for _, val := range *stringList {
		if *str == val {
			return true
		}
	}

	return false
}

func sendAnEmail(message string, subject string) {
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
	if subject == "" {
		e.Subject = "Issue detected when I convert some .mkv"
	} else {
		e.Subject = subject
	}
	e.Text = []byte(message)
	if err := e.Send(hostNPort, smtp.PlainAuth("", username, password, host)); err != nil {
		log.Warningf("Unable to send an email to \"%s\": %v", strings.Join(to, " "), err)
	} else {
		log.Debugf("Email was sent to \"%s\"", strings.Join(to, " "))
	}
}

func splitMP4File(filename *string) {
	log := logging.MustGetLogger("log")

	log.Debugf("I'm waiting a minute (empty buffer) and i'll split %s file", *filename)

	time.Sleep(time.Duration(viper.GetInt("default.sleeptimebuffer")) * time.Second)

	log.Debugf("I splitting file \"%s\"", *filename)

	cutTime := viper.GetInt("split.cuttime")

	cmd := fmt.Sprintf("ffmpeg -y -i %s -acodec copy -f segment -segment_time %d -vcodec copy -reset_timestamps 1 -map 0 %soutput%%d.mkv", *filename, cutTime, inProgessDir)
	exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
	// os.OpenFile(fmt.Sprintf("InProgress/%s", *filename), os.O_RDONLY|os.O_CREATE, 0666)
	os.OpenFile(path.Join(inProgessDir, *filename), os.O_RDONLY|os.O_CREATE, 0666)
}

func transformToMKV(filename *string) {
	log := logging.MustGetLogger("log")

	log.Debugf("Compressing file \"%s\"", *filename)

	codec := viper.GetString("quality.codec")
	crf := viper.GetInt("quality.crf")
	preset := viper.GetString("quality.preset")
	nice := viper.GetInt("default.nice")

	newFilename := strings.Replace(*filename, inProgessDir, beforeMergeDir, -1)
	cmd := fmt.Sprintf("nice -n %d ffmpeg -y -i %s -codec %s -crf %d -preset %s -c:a copy %s", nice, *filename, codec, crf, preset, newFilename)
	// cmd = fmt.Sprintf("nice -n %d ffmpeg -i %s -c:v copy -c:a copy %s", nice, *filename, newFilename)
	exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
}
