package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/agent"
	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/database"
	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/executor"
	"github.com/Yangsss13/flowpilot/internal/handler"
	"github.com/Yangsss13/flowpilot/internal/httpapi"
	"github.com/Yangsss13/flowpilot/internal/repository"
	"github.com/Yangsss13/flowpilot/internal/service"
	"github.com/Yangsss13/flowpilot/internal/workerpool"
)

type poolPublisher struct {
	pool *workerpool.Pool
}

type blockingPublisher struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func (p *blockingPublisher) Publish(_ context.Context, _ uint64) error {
	p.calls.Add(1)
	p.once.Do(func() { close(p.started) })
	<-p.release
	return nil
}

type integrationAgentPlanner struct{}

func (p *integrationAgentPlanner) CreatePlan(_ context.Context, _ string) (agent.Plan, error) {
	return agent.Plan{Steps: []agent.PlanStep{
		{ID: "search", Tool: agent.ToolRAGQuery, Input: json.RawMessage(`{"query":"refund policy"}`)},
		{ID: "fetch", Tool: agent.ToolHTTPRequest, Input: json.RawMessage(`{"method":"GET","url":"https://example.com"}`), DependsOn: []string{"search"}},
	}}, nil
}

func (p *poolPublisher) Publish(_ context.Context, taskID uint64) error {
	return p.pool.Submit(taskID)
}

func TestTaskEndpointsWithMySQL(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run MySQL integration tests")
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
	executionService := service.NewExecutionService(taskRepository, executionRepository, &poolPublisher{pool: pool})
	agentService := service.NewAgentService(&integrationAgentPlanner{}, taskRepository)
	router := httpapi.NewRouter(
		handler.NewTaskHandler(taskService),
		handler.NewExecutionHandler(executionService),
		handler.NewAgentHandler(agentService, nil),
		nil,
		handler.NewCapabilityHandler(true, agent.DefaultToolDefinitions(), false),
		handler.NewHealthHandler(map[string]handler.ReadinessCheck{}),
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
	var listed struct {
		Items []map[string]json.RawMessage `json:"items"`
		Total int64                        `json:"total"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode task list: %v", err)
	}
	if listed.Total < 1 {
		t.Fatalf("list total = %d", listed.Total)
	}
	for _, task := range listed.Items {
		var id uint64
		if err := json.Unmarshal(task["id"], &id); err == nil && id == created.ID {
			if _, hasSteps := task["steps"]; hasSteps {
				t.Fatal("list response unexpectedly includes steps")
			}
			var stepCount int64
			if err := json.Unmarshal(task["step_count"], &stepCount); err != nil || stepCount != 2 {
				t.Fatalf("step_count = %d error=%v", stepCount, err)
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
	if len(successLogs) != 7 {
		t.Fatalf("success log count = %d, want 7", len(successLogs))
	}

	retryResponse := performRequest(router, http.MethodPost, "/api/tasks/"+strconv.FormatUint(created.ID, 10)+"/run", nil)
	if retryResponse.Code != http.StatusConflict {
		t.Fatalf("successful task rerun status = %d, want 409", retryResponse.Code)
	}

	agentCreateResponse := performRequest(router, http.MethodPost, "/api/agent/tasks", []byte(`{"goal":"summarize refund policy"}`))
	if agentCreateResponse.Code != http.StatusCreated {
		t.Fatalf("agent create status = %d, want 201; body=%s", agentCreateResponse.Code, agentCreateResponse.Body.String())
	}
	var agentTask domain.Task
	if err := json.Unmarshal(agentCreateResponse.Body.Bytes(), &agentTask); err != nil {
		t.Fatalf("decode agent task: %v", err)
	}
	t.Cleanup(func() {
		db.Where("task_id = ?", agentTask.ID).Delete(&domain.TaskStep{})
		db.Delete(&domain.Task{}, agentTask.ID)
	})
	if agentTask.TaskType != domain.TaskTypeAgent || len(agentTask.Steps) != 2 || string(agentTask.Steps[1].DependsOn) != `["search"]` {
		t.Fatalf("unexpected agent task: %#v", agentTask)
	}
	agentRunResponse := performRequest(router, http.MethodPost, "/api/tasks/"+strconv.FormatUint(agentTask.ID, 10)+"/run", nil)
	if agentRunResponse.Code != http.StatusConflict {
		t.Fatalf("agent workflow run status = %d, want 409", agentRunResponse.Code)
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

func TestConcurrentRunRequestsOnlyQueueOnce(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run MySQL integration tests")
	}
	db, err := database.OpenMySQL(config.Load().Database)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate MySQL: %v", err)
	}
	task := &domain.Task{
		Name:     "concurrent-run-" + time.Now().Format("150405.000000000"),
		TaskType: domain.TaskTypeWorkflow, Status: domain.StatusPending,
		Steps: []domain.TaskStep{{Name: "wait", StepOrder: 1, ActionType: "sleep", ActionPayload: json.RawMessage(`{"duration_ms":1}`), Status: domain.StatusPending}},
	}
	tasks := repository.NewGormTaskRepository(db)
	if err := tasks.Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		db.Where("task_id = ?", task.ID).Delete(&domain.ExecutionLog{})
		db.Where("task_id = ?", task.ID).Delete(&domain.TaskStep{})
		db.Delete(&domain.Task{}, task.ID)
	})
	publisher := &blockingPublisher{started: make(chan struct{}), release: make(chan struct{})}
	execution := service.NewExecutionService(tasks, repository.NewGormExecutionRepository(db), publisher)
	router := gin.New()
	router.POST("/api/tasks/:id/run", handler.NewExecutionHandler(execution).Run)
	path := "/api/tasks/" + strconv.FormatUint(task.ID, 10) + "/run"
	statuses := make(chan int, 20)
	go func() { statuses <- performRequest(router, http.MethodPost, path, nil).Code }()
	select {
	case <-publisher.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not reach publisher")
	}
	for index := 1; index < 20; index++ {
		go func() { statuses <- performRequest(router, http.MethodPost, path, nil).Code }()
	}
	deadline := time.After(3 * time.Second)
	conflicts := 0
	for conflicts < 19 {
		select {
		case status := <-statuses:
			if status != http.StatusConflict {
				t.Fatalf("concurrent status = %d, want 409", status)
			}
			conflicts++
		case <-deadline:
			t.Fatalf("received %d conflicts, want 19", conflicts)
		}
	}
	close(publisher.release)
	if status := <-statuses; status != http.StatusAccepted {
		t.Fatalf("winning status = %d, want 202", status)
	}
	if publisher.calls.Load() != 1 {
		t.Fatalf("publisher calls = %d, want 1", publisher.calls.Load())
	}
	loaded, err := tasks.GetByID(context.Background(), task.ID)
	if err != nil || loaded.Status != domain.StatusQueued {
		t.Fatalf("queued task status=%v error=%v", loaded.Status, err)
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
