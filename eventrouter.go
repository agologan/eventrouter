/*
Copyright 2017 Heptio Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/agologan/eventrouter/sinks"
	"k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// EventRouter is responsible for maintaining a stream of kubernetes
// system Events and pushing them to another channel for storage
type EventRouter struct {
	// kubeclient is the main kubernetes interface
	kubeClient kubernetes.Interface

	// store of events populated by the shared informer
	eLister corelisters.EventLister

	// returns true if the event store has been synced
	eListerSynched cache.InformerSynced

	// event sink
	// TODO: Determine if we want to support multiple sinks.
	eSink sinks.EventSinkInterface
}

// NewEventRouter will create a new event router using the input params
func NewEventRouter(kubeClient kubernetes.Interface, eventsInformer coreinformers.EventInformer) *EventRouter {
	er := &EventRouter{
		kubeClient: kubeClient,
		eSink:      sinks.ManufactureSink(),
	}
	eventsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    er.addEvent,
		UpdateFunc: er.updateEvent,
		DeleteFunc: er.deleteEvent,
	})
	er.eLister = eventsInformer.Lister()
	er.eListerSynched = eventsInformer.Informer().HasSynced
	return er
}

// Run starts the EventRouter/Controller.
func (er *EventRouter) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer glog.Infof("Shutting down EventRouter")

	glog.Infof("Starting EventRouter")

	// here is where we kick the caches into gear
	if !cache.WaitForCacheSync(stopCh, er.eListerSynched) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}
	<-stopCh
}

// addEvent is called when an event is created, or during the initial list
func (er *EventRouter) addEvent(obj interface{}) {
	e, ok := obj.(*v1.Event)
	if !ok {
		glog.Error("Given object '%v' not v1.Event", obj)
		return
	}
	er.eSink.UpdateEvents(e, nil)
}

// updateEvent is called any time there is an update to an existing event
func (er *EventRouter) updateEvent(objOld interface{}, objNew interface{}) {
	eOld, ok := objOld.(*v1.Event)
	if !ok {
		glog.Error("Given object '%v' not v1.Event", objOld)
		return
	}
	eNew, ok := objNew.(*v1.Event)
	if !ok {
		glog.Error("Given object '%v' not v1.Event", objNew)
		return
	}
	if eOld.ResourceVersion == eNew.ResourceVersion {
		// change nothing so we can skip it
		return
	}
	er.eSink.UpdateEvents(eNew, eOld)
}

// deleteEvent should only occur when the system garbage collects events via TTL expiration
func (er *EventRouter) deleteEvent(obj interface{}) {
	e, ok := obj.(*v1.Event)
	if !ok {
		glog.Error("Given object '%v' not v1.Event", obj)
		return
	}
	// NOTE: This should *only* happen on TTL expiration there
	// is no reason to push this to a sink
	glog.V(5).Infof("Event Deleted from the system:\n%v", e)
}
