package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"github.com/moby/moby/client/pkg/jsonmessage"
	"github.com/moby/moby/client/pkg/progress"
	"github.com/moby/moby/client/pkg/streamformatter"
	"golang.org/x/term"
)

var numberedStates = map[swarm.TaskState]int64{
	swarm.TaskStateNew:       1,
	swarm.TaskStateAllocated: 2,
	swarm.TaskStatePending:   3,
	swarm.TaskStateAssigned:  4,
	swarm.TaskStateAccepted:  5,
	swarm.TaskStatePreparing: 6,
	swarm.TaskStateReady:     7,
	swarm.TaskStateStarting:  8,
	swarm.TaskStateRunning:   9,
	swarm.TaskStateComplete:  10,
	swarm.TaskStateShutdown:  11,
	swarm.TaskStateFailed:    12,
	swarm.TaskStateRejected:  13,
}

func terminalState(state swarm.TaskState) bool {
	return numberedStates[state] > numberedStates[swarm.TaskStateRunning]
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func waitOnServices(ctx context.Context, apiClient client.APIClient, services map[string]string, quiet bool, out io.Writer) error {
	ids := make([]string, 0, len(services))
	for id := range services {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return services[ids[i]] < services[ids[j]]
	})

	isTTY := false
	var fd uintptr
	if !quiet {
		if f, ok := out.(*os.File); ok {
			fd = f.Fd()
			isTTY = term.IsTerminal(int(fd))
		}
	}

	pipeReader, pipeWriter := io.Pipe()
	displayDone := make(chan error, 1)
	go func() {
		if quiet {
			_, err := io.Copy(io.Discard, pipeReader)
			displayDone <- err
			return
		}
		displayDone <- jsonmessage.DisplayJSONMessagesStream(pipeReader, out, fd, isTTY, nil)
	}()

	progressOut := streamformatter.NewJSONProgressOutput(pipeWriter, false)

	errCh := make(chan error, len(ids))
	var wg sync.WaitGroup
	for _, id := range ids {
		id := id
		name := services[id]
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- serviceProgress(ctx, apiClient, id, name, progressOut, isTTY)
		}()
	}
	wg.Wait()
	close(errCh)

	var runErr error
	for err := range errCh {
		runErr = errors.Join(runErr, err)
	}

	pipeWriter.Close()
	displayErr := <-displayDone

	if isTTY {
		fmt.Fprintln(out)
	}

	if runErr != nil {
		return runErr
	}
	return displayErr
}

func serviceProgress(ctx context.Context, apiClient client.APIClient, serviceID, displayName string, progressOut progress.Output, tty bool) error {
	var (
		updater     progressUpdater
		converged   bool
		convergedAt time.Time
		monitor     = 5 * time.Second
		rollback    bool
		frame       int
	)

	for {
		select {
		case <-ctx.Done():
			progress.Update(progressOut, displayName, "continuing in background")
			return nil
		default:
		}

		res, err := apiClient.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
		if err != nil {
			return err
		}

		if res.Service.Spec.UpdateConfig != nil && res.Service.Spec.UpdateConfig.Monitor != 0 {
			monitor = res.Service.Spec.UpdateConfig.Monitor
		}

		if updater == nil {
			updater = initializeUpdater(res.Service)
			if updater == nil {
				progress.Update(progressOut, displayName, "✓ converged")
				return nil
			}
		}

		if res.Service.UpdateStatus != nil {
			switch res.Service.UpdateStatus.State {
			case swarm.UpdateStateUpdating:
				rollback = false
			case swarm.UpdateStateCompleted:
				if !converged {
					progress.Update(progressOut, displayName, "✓ converged")
					return nil
				}
			case swarm.UpdateStatePaused:
				msg := fmt.Sprintf("update paused: %s", res.Service.UpdateStatus.Message)
				progress.Update(progressOut, displayName, "✗ "+msg)
				return fmt.Errorf("%s: %s", displayName, msg)
			case swarm.UpdateStateRollbackStarted:
				rollback = true
			case swarm.UpdateStateRollbackPaused:
				msg := fmt.Sprintf("rollback paused: %s", res.Service.UpdateStatus.Message)
				progress.Update(progressOut, displayName, "✗ "+msg)
				return fmt.Errorf("%s: %s", displayName, msg)
			case swarm.UpdateStateRollbackCompleted:
				rollback = true
			}
		}
		if converged && time.Since(convergedAt) >= monitor {
			progress.Update(progressOut, displayName, "✓ converged")
			return nil
		}

		tasksRes, err := apiClient.TaskList(ctx, client.TaskListOptions{
			Filters: make(client.Filters).Add("service", res.Service.ID).Add("_up-to-date", "true"),
		})
		if err != nil {
			return err
		}
		activeNodes, err := getActiveNodes(ctx, apiClient)
		if err != nil {
			return err
		}

		total, states, uErr := updater.update(res.Service, tasksRes.Items, activeNodes)
		if uErr != nil {
			progress.Update(progressOut, displayName, "✗ failed: "+uErr.Error())
			return uErr
		}

		var running, starting, failed int
		for s, n := range states {
			switch s {
			case swarm.TaskStateRunning, swarm.TaskStateComplete:
				running += n
			case swarm.TaskStateFailed, swarm.TaskStateRejected:
				failed += n
			default:
				starting += n
			}
		}

		if tty {
			prefix := ""
			if rollback {
				prefix = "rolling back: "
			}
			progress.Update(progressOut, displayName, fmt.Sprintf("%s %s%d/%d (%d starting / %d running / %d failed)",
				spinnerFrames[frame%len(spinnerFrames)], prefix, running, total, starting, running, failed))
			frame++
		}

		if total > 0 && running == total {
			if convergedAt.IsZero() {
				convergedAt = time.Now()
			}
			converged = true
		} else {
			convergedAt = time.Time{}
			converged = false
		}

		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			progress.Update(progressOut, displayName, "continuing in background")
			return nil
		}
	}
}

func getActiveNodes(ctx context.Context, apiClient client.APIClient) (map[string]struct{}, error) {
	res, err := apiClient.NodeList(ctx, client.NodeListOptions{})
	if err != nil {
		return nil, err
	}
	active := make(map[string]struct{})
	for _, n := range res.Items {
		if n.Status.State != swarm.NodeStateDown {
			active[n.ID] = struct{}{}
		}
	}
	return active, nil
}

// progressUpdater dedups tasks per slot/node and returns the total expected
// plus a histogram of the live tasks' Status.State. Tasks whose DesiredState
// is terminal (being torn down) are excluded from the histogram.
type progressUpdater interface {
	update(service swarm.Service, tasks []swarm.Task, activeNodes map[string]struct{}) (total int, states map[swarm.TaskState]int, err error)
}

func initializeUpdater(service swarm.Service) progressUpdater {
	if service.Spec.Mode.Replicated != nil && service.Spec.Mode.Replicated.Replicas != nil {
		return &replicatedUpdater{}
	}
	if service.Spec.Mode.Global != nil {
		return &globalUpdater{}
	}
	return nil
}

type replicatedUpdater struct{}

func (u *replicatedUpdater) update(service swarm.Service, tasks []swarm.Task, activeNodes map[string]struct{}) (int, map[swarm.TaskState]int, error) {
	if service.Spec.Mode.Replicated == nil || service.Spec.Mode.Replicated.Replicas == nil {
		return 0, nil, fmt.Errorf("no replica count")
	}
	replicas := int(*service.Spec.Mode.Replicated.Replicas)

	tasksBySlot := make(map[int]swarm.Task)
	for _, task := range tasks {
		if numberedStates[task.DesiredState] == 0 || numberedStates[task.Status.State] == 0 {
			continue
		}
		if existing, ok := tasksBySlot[task.Slot]; ok {
			if numberedStates[existing.DesiredState] < numberedStates[task.DesiredState] {
				continue
			}
			if numberedStates[existing.DesiredState] == numberedStates[task.DesiredState] &&
				numberedStates[existing.Status.State] <= numberedStates[task.Status.State] {
				continue
			}
		}
		if task.NodeID != "" {
			if _, ok := activeNodes[task.NodeID]; !ok {
				continue
			}
		}
		tasksBySlot[task.Slot] = task
	}

	return replicas, tallyStates(tasksBySlot), nil
}

type globalUpdater struct{}

func (u *globalUpdater) update(_ swarm.Service, tasks []swarm.Task, activeNodes map[string]struct{}) (int, map[swarm.TaskState]int, error) {
	tasksByNode := make(map[string]swarm.Task)
	for _, task := range tasks {
		if numberedStates[task.DesiredState] == 0 || numberedStates[task.Status.State] == 0 {
			continue
		}
		if existing, ok := tasksByNode[task.NodeID]; ok {
			if numberedStates[existing.DesiredState] < numberedStates[task.DesiredState] {
				continue
			}
			if numberedStates[existing.DesiredState] == numberedStates[task.DesiredState] &&
				numberedStates[existing.Status.State] <= numberedStates[task.Status.State] {
				continue
			}
		}
		tasksByNode[task.NodeID] = task
	}

	active := make(map[string]swarm.Task, len(tasksByNode))
	for nodeID, t := range tasksByNode {
		if _, ok := activeNodes[nodeID]; ok {
			active[nodeID] = t
		}
	}

	return len(active), tallyStates(active), nil
}

func tallyStates[K comparable](tasks map[K]swarm.Task) map[swarm.TaskState]int {
	states := make(map[swarm.TaskState]int)
	for _, t := range tasks {
		if terminalState(t.DesiredState) {
			continue
		}
		states[t.Status.State]++
	}
	return states
}
