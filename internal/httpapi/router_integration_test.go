package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"minikvx-agent/internal/config"
	"minikvx-agent/internal/database"
	"minikvx-agent/internal/domain"
	"minikvx-agent/internal/executor"
	"minikvx-agent/internal/handler"
	"minikvx-agent/internal/httpapi"
	"minikvx-agent/internal/repository"
	"minikvx-agent/internal/service"
	"minikvx-agent/internal/workerpool"
)

func TestTaskEndpointsWithMySQL(t *testing.T) {
	if os.Getenv("MINIKVX_INTEGRATION") != "1" {
		t.Skip("set MINIKVX_INTEGRATION=1 to run MySQL integration tests")
	}

	db, err := database.OpenMySQL(config.Load().Database)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate MySQL: %v", err)
	}

	taskRepository := repository.NewGormTaskRepository(db)
	executionRepository := repository.NewGormExecutionRepository(db)
	taskService := service.NewTaskService(taskRepository)
	stepExecutor := executor.NewStepExecutor()
	taskExecutor := executor.NewTaskExecutor(taskRepository, executionRepository, stepExecutor)
	pool, err := workerpool.New(context.Background(), taskExecutor, 2, 10)
	if err != nil {
		t.Fatalf("create worker pool: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := pool.Stop(ctx); err != nil {
			t.Errorf("stop worker pool: %v", err)
		}
	})
	executionService := service.NewExecutionService(taskRepository, executionRepository, pool)
	router := httpapi.NewRouter(
		handler.NewTaskHandler(taskService),
		handler.NewExecutionHandler(executionService),
	)

	name := "query-integration-" + time.Now().Format("20060102150405.000000000")
	createBody := []byte(`{
		"name":"` + name + `",
		"steps":[
			{"name":"first","action_type":"sleep","action_payload":{"duration_ms":1}},
			{"name":"second","action_type":"http_mock","action_payload":{"status":200}}
		]
	}`)
	createResponse := performRequest(router, http.MethodPost, "/api/tasks", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body=%s", createResponse.Code, createResponse.Body.String())
	}

	var created domain.Task
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created task: %v", err)
	}
	t.Cleanup(func() {
		db.Where("task_id = ?", created.ID).Delete(&domain.TaskStep{})
		db.Delete(&domain.Task{}, created.ID)
	})

	listResponse := performRequest(router, http.MethodGet, "/api/tasks", nil)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listResponse.Code)
	}
	var listed []map[string]json.RawMessage
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode task list: %v", err)
	}
	for _, task := range listed {
		var id uint64
		if err := json.Unmarshal(task["id"], &id); err == nil && id == created.ID {
			if _, hasSteps := task["steps"]; hasSteps {
				t.Fatal("list response unexpectedly includes steps")
			}
		}
	}

	detailResponse := performRequest(router, http.MethodGet, "/api/tasks/"+strconv.FormatUint(created.ID, 10), nil)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", detailResponse.Code)
	}
	var detail domain.Task
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode task detail: %v", err)
	}
	if len(detail.Steps) != 2 || detail.Steps[0].StepOrder != 1 || detail.Steps[1].StepOrder != 2 {
		t.Fatalf("detail steps are not in business order: %#v", detail.Steps)
	}

	notFoundResponse := performRequest(router, http.MethodGet, "/api/tasks/18446744073709551615", nil)
	if notFoundResponse.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d, want 404", notFoundResponse.Code)
	}

	runResponse := performRequest(router, http.MethodPost, "/api/tasks/"+strconv.FormatUint(created.ID, 10)+"/run", nil)
	if runResponse.Code != http.StatusAccepted {
		t.Fatalf("successful run status = %d, want 202; body=%s", runResponse.Code, runResponse.Body.String())
	}
	succeeded := waitForTerminalTask(t, router, created.ID)
	if succeeded.Status != domain.StatusSuccess || len(succeeded.Steps) != 2 || succeeded.Steps[0].Status != domain.StatusSuccess || succeeded.Steps[1].Status != domain.StatusSuccess {
		t.Fatalf("unexpected successful task state: %#v", succeeded)
	}

	logsResponse := performRequest(router, http.MethodGet, "/api/tasks/"+strconv.FormatUint(created.ID, 10)+"/logs", nil)
	if logsResponse.Code != http.StatusOK {
		t.Fatalf("logs status = %d, want 200", logsResponse.Code)
	}
	var successLogs []domain.ExecutionLog
	if err := json.Unmarshal(logsResponse.Body.Bytes(), &successLogs); err != nil {
		t.Fatalf("decode success logs: %v", err)
	}
	if len(successLogs) != 6 {
		t.Fatalf("success log count = %d, want 6", len(successLogs))
	}

	retryResponse := performRequest(router, http.MethodPost, "/api/tasks/"+strconv.FormatUint(created.ID, 10)+"/run", nil)
	if retryResponse.Code != http.StatusConflict {
		t.Fatalf("successful task rerun status = %d, want 409", retryResponse.Code)
	}

	failureBody := []byte(`{
		"name":"failure-integration",
		"steps":[
			{"name":"first","action_type":"sleep","action_payload":{"duration_ms":1}},
			{"name":"fails","action_type":"http_mock","action_payload":{"status":500}},
			{"name":"not reached","action_type":"shell_mock","action_payload":{"exit_code":0}}
		]
	}`)
	failureCreateResponse := performRequest(router, http.MethodPost, "/api/tasks", failureBody)
	if failureCreateResponse.Code != http.StatusCreated {
		t.Fatalf("failure task create status = %d, want 201; body=%s", failureCreateResponse.Code, failureCreateResponse.Body.String())
	}
	var failureTask domain.Task
	if err := json.Unmarshal(failureCreateResponse.Body.Bytes(), &failureTask); err != nil {
		t.Fatalf("decode failure task: %v", err)
	}
	t.Cleanup(func() {
		db.Where("task_id = ?", failureTask.ID).Delete(&domain.ExecutionLog{})
		db.Where("task_id = ?", failureTask.ID).Delete(&domain.TaskStep{})
		db.Delete(&domain.Task{}, failureTask.ID)
	})

	failureRunResponse := performRequest(router, http.MethodPost, "/api/tasks/"+strconv.FormatUint(failureTask.ID, 10)+"/run", nil)
	if failureRunResponse.Code != http.StatusAccepted {
		t.Fatalf("failed workflow HTTP status = %d, want 202; body=%s", failureRunResponse.Code, failureRunResponse.Body.String())
	}
	failed := waitForTerminalTask(t, router, failureTask.ID)
	if failed.Status != domain.StatusFailed || failed.Steps[0].Status != domain.StatusSuccess || failed.Steps[1].Status != domain.StatusFailed || failed.Steps[2].Status != domain.StatusPending {
		t.Fatalf("unexpected failed task state: %#v", failed)
	}
}

func waitForTerminalTask(t *testing.T, router http.Handler, taskID uint64) domain.Task {
	t.Helper()
	path := "/api/tasks/" + strconv.FormatUint(taskID, 10)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response := performRequest(router, http.MethodGet, path, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("poll task status = %d, want 200", response.Code)
		}
		var task domain.Task
		if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
			t.Fatalf("decode polled task: %v", err)
		}
		if task.Status == domain.StatusSuccess || task.Status == domain.StatusFailed {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %d did not reach a terminal state", taskID)
	return domain.Task{}
}

func performRequest(router http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
