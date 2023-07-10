package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const (
	ua               = "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36" // Midjourney-history-sync/1.4" // the user agent string
	userIDFile       = "userid.txt"                                                                                                 // file name for user ID
	sessionTokenFile = "sessiontoken.txt"                                                                                           // file name for user token
	jobsDir          = "jobs"                                                                                                       // directory name for storing downloaded job data
	imagesPerPage    = 20
)

var (
	a             fyne.App
	galleryWindow fyne.Window
	currentIndex  int = 0
	logTextArea   *widget.Label
	currentPage   int
	totalPages    int
	allImages     [][]fyne.CanvasObject
	jobsType      = "new" // orderBy value in MidJourney JSON (new || top-all)
	activeJob     = true
)

type Job struct {
	ID          string   `json:"id"`           // the unique identifier of a job
	EnqueueTime string   `json:"enqueue_time"` // when the job was added to a queue
	ImagePaths  []string `json:"image_paths"`  // the paths of the images associated with the job
}

func main() {
	if len(os.Args) > 1 {
		jobsType = os.Args[1]
	}
	a = app.New()
	w := a.NewWindow("MidJourney Downloader")
	w.SetMaster()        // Keep the app always on top
	w.SetFixedSize(true) // Don't resize the app

	userIDEntry := widget.NewEntry()
	userIDEntry.SetPlaceHolder("User ID")

	sessionTokenEntry := &widget.Entry{MultiLine: true, Wrapping: fyne.TextWrapWord}
	sessionTokenEntry.SetPlaceHolder("Session Token")

	loadFileContent("userid.txt", userIDEntry)
	loadFileContent("sessiontoken.txt", sessionTokenEntry)

	saveUserIDBtn := widget.NewButton("Save", func() {
		saveFileContent("userid.txt", userIDEntry.Text)
		w.SetContent(createSessionTokenScreen(sessionTokenEntry, w))
	})

	galleryButton := widget.NewButton("Afficher la galerie", func() {
		showGallery(a)
	})

	logTextArea = widget.NewLabel("")
	logTextArea.TextStyle = fyne.TextStyle{Bold: true}

	logWindow := a.NewWindow("Logs")
	logWindow.SetContent(container.NewScroll(logTextArea))
	logWindow.Resize(fyne.NewSize(500, 500))

	logButton := widget.NewButton("Show Logs", func() {
		logWindow.Show()
	})

	userIDBox := container.NewVBox(userIDEntry, saveUserIDBtn, logButton, galleryButton)

	ticker := time.NewTicker(1 * time.Hour)
	quit := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				recentJobs()
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	w.SetOnClosed(func() {
		quit <- struct{}{}
	})

	w.SetContent(userIDBox)
	w.Resize(fyne.NewSize(300, 130))
	w.ShowAndRun()
}

func loadImageResource(path string) (fyne.Resource, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return fyne.NewStaticResource(filepath.Base(path), bytes), nil
}

func getImages() error {
	// obtenir tous les fichiers png du répertoire de travail
	files, err := filepath.Glob(filepath.Join(jobsDir, "*.png"))
	if err != nil {
		logToWindow("Erreur lors de la récupération des fichiers PNG: " + err.Error())
		return err
	}

	// trier les fichiers par nom pour s'assurer qu'ils sont dans le bon ordre
	sort.Strings(files)

	// s'assurer que nous ne dépassons pas la fin de la tranche
	end := currentIndex + imagesPerPage
	if end > len(files) {
		end = len(files)
	}

	// obtenir seulement les fichiers dont nous avons besoin
	files = files[currentIndex:end]

	var images []fyne.CanvasObject
	for _, file := range files {
		resource, err := loadImageResource(file)
		if err != nil {
			logToWindow("Erreur lors de la création de la ressource image: " + err.Error())
			return err
		}

		imageButton := widget.NewButtonWithIcon("", resource, func() {
			url, _ := url.Parse("file://" + file)
			a.OpenURL(url)
		})
		images = append(images, imageButton)
	}

	allImages = append(allImages, images)
	return nil
}

func createGallery(a fyne.App, currentPage int) fyne.CanvasObject {

	prevButton := widget.NewButton("<", func() {
		if currentPage > 0 {
			currentPage--
			currentIndex -= imagesPerPage
			err := getImages()
			if err != nil {
				logToWindow("Erreur lors de la récupération des images : " + err.Error())
				return
			}
			galleryWindow.SetContent(createGallery(a, currentPage))
		}
	})

	nextButton := widget.NewButton(">", func() {
		if currentPage < totalPages-1 {
			currentPage++
			currentIndex += imagesPerPage
			err := getImages()
			if err != nil {
				logToWindow("Erreur lors de la récupération des images : " + err.Error())
				return
			}
			galleryWindow.SetContent(createGallery(a, currentPage))
		}
	})

	// Création de la grille d'images avec chaque image prenant 100% de sa case
	imageObjects := make([]fyne.CanvasObject, len(allImages[currentPage]))
	for i, img := range allImages[currentPage] {
		imageObjects[i] = container.NewMax(img)
	}

	gallery := container.NewGridWithColumns(5, imageObjects...)
	return container.NewBorder(nil, container.NewHBox(prevButton, nextButton), nil, nil, gallery)
}

func showGallery(a fyne.App) {
	err := getImages()
	if err != nil {
		logToWindow("Erreur lors de la récupération des images:" + err.Error())
		return
	}

	galleryWindow = a.NewWindow("Galerie")
	galleryWindow.SetContent(createGallery(a, currentPage))
	galleryWindow.Resize(fyne.NewSize(500, 500))
	galleryWindow.Show()
}

func logToWindow(message string) {
	if logTextArea != nil {
		logTextArea.SetText(logTextArea.Text + "\n" + message)
	}
}

func createSessionTokenScreen(entry *widget.Entry, w fyne.Window) fyne.CanvasObject {
	saveSessionTokenBtn := widget.NewButton("Save", func() {
		saveFileContent("sessiontoken.txt", entry.Text)
		w.SetContent(createProcessJobsScreen(w))
	})

	return container.NewVBox(entry, saveSessionTokenBtn)
}

func createProcessJobsScreen(w fyne.Window) fyne.CanvasObject {
	activeJob = true

	spinner := widget.NewProgressBarInfinite()
	go func() {
		recentJobs()
		w.SetContent(createFinishedScreen(w))
	}()

	return container.NewCenter(spinner)
}

func createFinishedScreen(w fyne.Window) fyne.CanvasObject {
	closeBtn := widget.NewButton("Close", func() {
		w.Close()
	})
	refreshBtn := widget.NewButton("Refresh", func() {
		w.SetContent(createProcessJobsScreen(w))
	})
	return container.NewVBox(container.NewHBox(container.NewCenter(refreshBtn), container.NewCenter(closeBtn)))

}

func loadFileContent(filename string, entry *widget.Entry) {
	content, err := os.ReadFile(filename)
	if err == nil {
		entry.SetText(string(content))
	}
}

func saveFileContent(filename, content string) {
	err := os.WriteFile(filename, []byte(content), 0644)
	if err != nil {
		logToWindow("Erreur lors de l'enregistrement du fichier : " + err.Error())
	}
}

// The function reads user ID and session token from files, fetches jobs data from an API, writes the
// data to files, and processes each job.
func recentJobs() {
	if !activeJob {
		return
	}

	userID, err := os.ReadFile(userIDFile)
	if err != nil {
		logToWindow("[801] " + err.Error())
		return
	}

	sessionToken, err := os.ReadFile(sessionTokenFile)
	if err != nil {
		logToWindow("[802] " + err.Error())
		return
	}

	os.MkdirAll(jobsDir, 0755)

	for page := 1; page <= 200; page++ {
		jobs, err := fetchJobs(strings.TrimSpace(string(userID)), strings.TrimSpace(string(sessionToken)), page)
		if err != nil {
			logToWindow("[803] " + err.Error())
			return
		}

		lastPageFile := fmt.Sprintf("jobs/last_page_%d.json", page)
		err = os.WriteFile(lastPageFile, jobs, 0644)
		if err != nil {
			logToWindow("[804] " + err.Error())
			return
		}

		var jobArray []Job
		err = json.Unmarshal(jobs, &jobArray)
		if err != nil {
			logToWindow("[805] " + err.Error())
			return
		}

		for _, job := range jobArray {
			if activeJob {
				processJob(job)
			}
		}
	}
}

// The function fetches recent completed jobs from a website's API using a user ID, session token, and
// page number.
func fetchJobs(userID, sessionToken string, page int) ([]byte, error) {
	if activeJob {

		req, err := http.NewRequest("GET", fmt.Sprintf("https://www.midjourney.com/api/app/recent-jobs/?orderBy="+jobsType+"&jobStatus=completed&userId=%s&dedupe=true&refreshApi=0&page=%d", userID, page), nil)
		if err != nil {
			logToWindow(fmt.Sprintf("1 https://www.midjourney.com/api/app/recent-jobs/?orderBy="+jobsType+"&jobStatus=completed&userId=%s&dedupe=true&refreshApi=0&page=%d", userID, page))
			return nil, err
		}

		req.Header.Set("User-Agent", ua)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", fmt.Sprintf("__Secure-next-auth.session-token=%s", sessionToken))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logToWindow(fmt.Sprintf("2 https://www.midjourney.com/api/app/recent-jobs/?orderBy="+jobsType+"&jobStatus=completed&userId=%s&dedupe=true&refreshApi=0&page=%d", userID, page))
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			logToWindow(fmt.Sprintf("3 https://www.midjourney.com/api/app/recent-jobs/?orderBy="+jobsType+"&jobStatus=completed&userId=%s&dedupe=true&refreshApi=0&page=%d", userID, page))
			return nil, err
		}

		return body, nil
	} else {
		return []byte{}, nil
	}
}

// The function processes a job by validating its ID and image paths, creating a directory for the job,
// downloading the images, and marking the job as completed.
func processJob(job Job) {
	jobID := job.ID
	jobIDPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if !jobIDPattern.MatchString(jobID) {
		logToWindow("Potentially unsafe job ID " + jobID + " -- stopping!")
		activeJob = false
		return
	}

	tdir := fmt.Sprintf("%s/%s", jobsDir, jobID)
	tdirInfo, err := os.Stat(tdir)
	if err == nil && tdirInfo.IsDir() {
		if _, err := os.Stat(fmt.Sprintf("%s/completed", tdir)); err == nil {
			logToWindow("Skipping " + jobID + " -- already downloaded.")
			return
		} else {
			logToWindow("Warning: " + jobID + " did not finish syncing. Will try again!")
		}
	} else {
		logToWindow("Téléchargement de " + jobID)
		os.Mkdir(tdir, 0755)
	}

	jobFile := fmt.Sprintf("%s/job.json", tdir)
	jobJSON, err := json.Marshal(job)
	if err != nil {
		panic(err)
	}
	err = os.WriteFile(jobFile, jobJSON, 0644)
	if err != nil {
		panic(err)
	}

	for _, imgURL := range job.ImagePaths {
		fname := strings.Split(imgURL, "/")[len(strings.Split(imgURL, "/"))-1]
		fnamePattern := regexp.MustCompile(`^[0-9_]+\.(png|jpg|jpeg|webp)$`)
		if !fnamePattern.MatchString(fname) {
			logToWindow("Chemin d'image potentiellement dangereux " + imgURL + " se terminant par " + fname + " -- arrêt!")
			os.Exit(1)
		}

		logToWindow("[200] " + fname)
		err := downloadImage(imgURL, fmt.Sprintf("%s/%s", tdir, fname))
		if err != nil {
			panic(err)
		}
	}

	err = os.WriteFile(fmt.Sprintf("%s/completed", tdir), []byte(""), 0644)
	if err != nil {
		panic(err)
	}
}

// The function downloads an image from a given URL and saves it to a specified destination.
func downloadImage(imgURL, dest string) error {
	resp, err := http.Get(imgURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	imgData, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	err = os.WriteFile(dest, imgData, 0644)
	if err != nil {
		return err
	}

	return nil
}

func Test() {
	// Simulate a long-running process
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Replace the following line with your actual job processing code
		time.Sleep(5 * time.Second)
	}()
	wg.Wait()
}
