package main

import (
	"container/heap"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Job represents a job with a name, duration,id and status
type Job struct {
	Name     string        `json:"name"`
	Duration time.Duration `json:"duration"`
	Status   string        `json:"status"`
	Id       string        `json:"id"`
}

// JobQueue implements a priority queue for Jobs
type JobQueue []*Job

func (jq JobQueue) Len() int           { return len(jq) }
func (jq JobQueue) Less(i, j int) bool { return jq[i].Duration < jq[j].Duration }
func (jq JobQueue) Swap(i, j int)      { jq[i], jq[j] = jq[j], jq[i] }
func (jq *JobQueue) Push(x interface{}) {
	*jq = append(*jq, x.(*Job))
}
func (jq *JobQueue) Pop() interface{} {
	old := *jq
	n := len(old)
	item := old[n-1]
	*jq = old[0 : n-1]
	return item
}

var (
	jobs     JobQueue
	jobMutex sync.Mutex
	clients  = make(map[*websocket.Conn]bool)
	wsMutex  sync.Mutex
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			// Allowing all origins for now
			return true
		}}
	//to keep track of  jobs with diffrent status
	completedJobs []*Job
	pendingJobs   []*Job
	runningJobs   []*Job
)

func main() {
	log.Printf("Server runnning on : 8080")
	jobs = make(JobQueue, 0)
	heap.Init(&jobs)

	http.HandleFunc("/jobs", handleJobs)
	http.HandleFunc("/ws", handleWebSocket)

	go processJobs()

	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleJobs(w http.ResponseWriter, r *http.Request) {
	// CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS,DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// Handle OPTIONS request
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("API hit: %s %s", r.Method, r.URL.Path)

	switch r.Method {
	case http.MethodPost: //adds a new job
		submitJobHandler(w, r)
	case http.MethodGet: //fetches all jobs
		getJobsHandler(w)
	case http.MethodDelete: //remove all jobs
		resetAllJobs()

	default:
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

func submitJobHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Handling submit job request")
	var job Job
	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		log.Printf("Error decoding JSON: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("Decoded Job: %+v", job)

	job.Status = "pending"
	job.Duration = job.Duration * time.Second // Converts seconds to nanoseconds
	job.Id = uuid.New().String()              //adds a unique id for each job
	pendingJobs = append(pendingJobs, &job)   //move it to pending state
	jobMutex.Lock()

	// Push job onto the heap
	heap.Push(&jobs, &job)
	jobMutex.Unlock()
	broadcastJobStatus(&job) //broadcast to frontend that a job with pending status is added
	log.Println("Job added to the queue:", job)
	// Log current job queue status
	logJobQueue()
	// Respond with HTTP status 202 Accepted
	w.WriteHeader(http.StatusAccepted)
}

func getJobsHandler(w http.ResponseWriter) {

	// Combine pending jobs and completed jobs and runningjobs for response

	combinedJobs := append(completedJobs, pendingJobs...)
	combinedJobs = append(combinedJobs, runningJobs...)

	w.Header().Set("Content-Type", "application/json")

	if combinedJobs == nil {
		log.Println("No jobs available")
		combinedJobs = make(JobQueue, 0) // Initialize as empty slice to avoid nil JSON response
	}

	if err := json.NewEncoder(w).Encode(combinedJobs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logJobQueue()

}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS,DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// Handle OPTIONS request
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		log.Printf("Upgrade error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	wsMutex.Lock()
	clients[conn] = true
	wsMutex.Unlock()

	log.Println("WebSocket connection established")

}

func broadcastJobStatus(job *Job) { // used to broadcast a job status
	jobMutex.Lock()
	data, err := json.Marshal(job)

	jobMutex.Unlock()
	if err != nil {
		log.Printf("Error encoding job data: %v", err)
		jobMutex.Unlock()
		return
	}
	for client := range clients {
		if err := client.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("Error writing message to client: %v", err)
			client.Close()
			delete(clients, client)
		}
	}
}

func processJobs() { //keep running to make sure jobs are being processed
	for {
		jobMutex.Lock()
		if len(jobs) > 0 {
			job := heap.Pop(&jobs).(*Job)          //takes the smallest job
			removePendingJob(job.Id)               // remove it from pending jobs
			job.Status = "running"                 //update status to running
			runningJobs = append(runningJobs, job) //add it to running jobs array
			jobMutex.Unlock()
			broadcastJobStatus(job) //tell frontend its now runnning
			log.Printf("Processing job: %s", job.Name)
			time.Sleep(job.Duration) //sleep for time of job
			jobMutex.Lock()
			runningJobs = []*Job{} //empty the running jobs array
			job.Status = "completed"
			completedJobs = append(completedJobs, job) // Store completed job separately
			jobMutex.Unlock()
			broadcastJobStatus(job) //tell frontend its now completed
			log.Printf("Completed job: %s", job.Name)
			logJobQueue()
		} else {
			jobMutex.Unlock()
			time.Sleep(1 * time.Second) // Wait before checking for new jobs
		}
	}
}

func removePendingJob(id string) {
	for i, job := range pendingJobs {
		if job.Id == id {
			// Remove job from the slice
			pendingJobs = append(pendingJobs[:i], pendingJobs[i+1:]...)
			break
		}
	}
}
func resetAllJobs() {
	jobMutex.Lock()

	// Clear jobs in the heap
	jobs = make(JobQueue, 0)
	heap.Init(&jobs)

	// Clear pending jobs
	pendingJobs = []*Job{}

	// Clear running jobs
	runningJobs = []*Job{}

	// Clear completed jobs
	completedJobs = []*Job{}

	// Broadcast job status reset to all clients
	for client := range clients { //if reset call is success frontend will handle state update
		if err := client.WriteMessage(websocket.TextMessage, []byte("[]")); err != nil {
			log.Printf("Error writing reset message to client: %v", err)
			client.Close()
			delete(clients, client)
		}
	}
	jobMutex.Unlock()

	log.Println("All jobs have been reset")
}

func logJobQueue() {
	jobMutex.Lock()
	log.Println("Current job queue:")
	for i, job := range jobs {
		log.Printf("%d: %+v", i, job)
	}
	jobMutex.Unlock()

}
