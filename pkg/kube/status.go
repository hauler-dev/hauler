package kube

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/cli-utils/pkg/kstatus/polling"
	"sigs.k8s.io/cli-utils/pkg/kstatus/polling/aggregator"
	"sigs.k8s.io/cli-utils/pkg/kstatus/polling/collector"
	"sigs.k8s.io/cli-utils/pkg/kstatus/polling/event"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"strings"
	"time"
)

type StatusChecker struct {
	poller *polling.StatusPoller
	client client.Client
	logger *logrus.Entry

	interval time.Duration
	timeout time.Duration
}

func NewStatusChecker(kubeConfig *rest.Config, interval time.Duration, timeout time.Duration) (*StatusChecker, error) {
	restMapper, err := apiutil.NewDynamicRESTMapper(kubeConfig)
	if err != nil {
		return nil, err
	}

	c, err := client.New(kubeConfig, client.Options{Mapper: restMapper})
	if err != nil {
		return nil, err
	}

	return &StatusChecker{
		poller:   polling.NewStatusPoller(c, restMapper),
		client:   c,
		logger:   logrus.WithFields(logrus.Fields{}),
		interval: interval,
		timeout:  timeout,
	}, nil
}

func (sc *StatusChecker) WaitForCondition(objs ...object.ObjMetadata) error {
	ctx, cancel := context.WithTimeout(context.Background(), sc.timeout)
	defer cancel()

	eventsChan := sc.poller.Poll(ctx, objs, polling.Options{
		PollInterval: sc.interval,
		UseCache: true,
	})
	coll := collector.NewResourceStatusCollector(objs)

	done := coll.ListenWithObserver(eventsChan, desiredStatusNotifierFunc(cancel, status.CurrentStatus))
	<-done

	for _, rs := range coll.ResourceStatuses {
		switch rs.Status {
		case status.CurrentStatus:
			fmt.Printf("%s: %s ready\n", rs.Identifier.Name, strings.ToLower(rs.Identifier.GroupKind.Kind))
		case status.NotFoundStatus:
			fmt.Println(fmt.Errorf("%s: %s not found", rs.Identifier.Name, strings.ToLower(rs.Identifier.GroupKind.Kind)))
		default:
			fmt.Println(fmt.Errorf("%s: %s not ready", rs.Identifier.Name, strings.ToLower(rs.Identifier.GroupKind.Kind)))
		}
	}

	if coll.Error != nil || ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timed out waiting for condition")
	}

	return nil
}

// desiredStatusNotifierFunc returns an Observer function for the
// ResourceStatusCollector that will cancel the context (using the cancelFunc)
// when all resources have reached the desired status.
func desiredStatusNotifierFunc(cancelFunc context.CancelFunc, desired status.Status) collector.ObserverFunc {
	return func(rsc *collector.ResourceStatusCollector, _ event.Event) {
		var rss []*event.ResourceStatus
		for _, rs := range rsc.ResourceStatuses {
			rss = append(rss, rs)
		}
		aggStatus := aggregator.AggregateStatus(rss, desired)
		if aggStatus == desired {
			cancelFunc()
		}
	}
}
