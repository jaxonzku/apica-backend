# apica-backend

# Getting Started with Job Queue Management System

Available Scripts
In the project directory, you can run:

### git clone [https://github.com/jaxonzku/apica-backend]

### `cd apica-backend`

### `go run server.go`

Runs the app in development mode.
The server will start on port 8080.

## API Endpoints

### POST /jobs

Submit a new job. The request body should be a JSON object with the following structure:

json
Copy code
{
"name": "Job Name",
"duration": 10 // Duration in seconds
}

### GET /jobs

Fetch the list of all jobs. The response will be a JSON array containing all jobs with their current statuses:

json
Copy code
[
{
"name": "Job Name",
"duration": 10,
"status": "pending",
"id": "unique-job-id"
}
]

### DELETE /jobs

Remove all jobs. This will clear all pending, running, and completed jobs.

WebSocket Endpoint
ws://localhost:8080/ws
Connect to this WebSocket endpoint to receive real-time updates about job statuses.
