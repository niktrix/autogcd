package autogcd

import (
	"encoding/json"
	"github.com/wirepair/gcd"
	"github.com/wirepair/gcd/gcdapi"
	"log"
	"sync"
)

// when we are unable to find an element/nodeId
type ElementNotFoundErr struct {
	Message string
}

func (e *ElementNotFoundErr) Error() string {
	return "Unable to find element: " + e.Message
}

// when we are unable to access a tab
type InvalidTabErr struct {
	Message string
}

func (e *InvalidTabErr) Error() string {
	return "Unable to access tab: " + e.Message
}

type GcdResponseFunc func(target *gcd.ChromeTarget, payload []byte)

type ConsoleMessageFunc func(tab *Tab, message *gcdapi.ConsoleConsoleMessage)

type Tab struct {
	*gcd.ChromeTarget
	eleMutex *sync.Mutex
	Elements map[int]*Element
}

func NewTab(target *gcd.ChromeTarget) *Tab {
	t := &Tab{ChromeTarget: target}
	t.eleMutex = &sync.Mutex{}
	t.Elements = make(map[int]*Element)
	t.Page.Enable()
	t.DOM.Enable()
	t.Console.Enable()
	return t
}

func (t *Tab) ListenNodeChanges() {
	// new nodes
	t.Subscribe("DOM.setChildNodes", func(target *gcd.ChromeTarget, payload []byte) {
		log.Printf("SETNODE EVENT: %s\n", string(payload))
		message := &gcdapi.DOMSetChildNodesEvent{}
		header := &DefaultEventHeader{}
		err := json.Unmarshal(payload, header)
		if err == nil {
			message = header.Params.(*gcdapi.DOMSetChildNodesEvent)
			log.Printf("Got Message: %s\n", message)
		}

	})

	t.Subscribe("DOM.attributeModifiedEvent", func(target *gcd.ChromeTarget, payload []byte) {
		log.Printf("attributeModifiedEvent EVENT: %s\n", string(payload))
	})
	t.Subscribe("DOM.attributeRemovedEvent", func(target *gcd.ChromeTarget, payload []byte) {
		log.Printf("attributeRemovedEvent EVENT: %s\n", string(payload))
	})
	t.Subscribe("DOM.characterDataModifiedEvent", func(target *gcd.ChromeTarget, payload []byte) {
		log.Printf("characterDataModifiedEvent EVENT: %s\n", string(payload))
	})
	t.Subscribe("DOM.childNodeCountUpdatedEvent", func(target *gcd.ChromeTarget, payload []byte) {
		log.Printf("childNodeCountUpdatedEvent EVENT: %s\n", string(payload))
	})
	t.Subscribe("DOM.childNodeInsertedEvent", func(target *gcd.ChromeTarget, payload []byte) {
		log.Printf("childNodeInsertedEvent EVENT: %s\n", string(payload))
	})
	t.Subscribe("DOM.childNodeRemovedEvent", func(target *gcd.ChromeTarget, payload []byte) {
		log.Printf("childNodeRemovedEvent EVENT: %s\n", string(payload))
	})
	t.Subscribe("DOM.inlineStyleInvalidatedEvent", func(target *gcd.ChromeTarget, payload []byte) {
		log.Printf("inlineStyleInvalidatedEvent EVENT: %s\n", string(payload))
	})
	// node ids are no longer valid
	t.Subscribe("DOM.documentUpdated", func(target *gcd.ChromeTarget, payload []byte) {
		log.Printf("documentUpdated, deleting all nodes")
		t.eleMutex.Lock()
		t.Elements = make(map[int]*Element)
		t.eleMutex.Unlock()
	})
}

// Navigates to a URL and does not return until the Page.loadEventFired event.
// Returns the frameId of the Tab that this navigation occured on.
func (t *Tab) Navigate(url string) (string, error) {
	resp := make(chan int, 1)
	t.Subscribe("Page.loadEventFired", t.defaultLoadFired(resp))

	frameId, err := t.Page.Navigate(url)
	if err != nil {
		return "", err
	}
	<-resp
	return frameId, nil
}

// Registers chrome to start retrieving console messages, caller must pass in call back
// function to handle it.
func (t *Tab) GetConsoleMessages(messageHandler ConsoleMessageFunc) {
	t.Subscribe("Console.messageAdded", t.defaultConsoleMessageAdded(messageHandler))
}

// Stops the debugger service from sending console messages and closes the channel
func (t *Tab) StopConsoleMessages() {
	t.Unsubscribe("Console.messageAdded")
}

// Returns the top window documents source, as visible
func (t *Tab) GetPageSource() (string, error) {
	var node *gcdapi.DOMNode
	var err error
	node, err = t.DOM.GetDocument()
	if err != nil {
		return "", err
	}
	return t.DOM.GetOuterHTML(node.NodeId)
}

// Get all Elements that match a selector from the top level document
func (t *Tab) GetElementsBySelector(selector string) ([]*Element, error) {
	// get document
	docNode, err := t.DOM.GetDocument()
	if err != nil {
		return nil, err
	}

	nodeIds, errQuery := t.DOM.QuerySelectorAll(docNode.NodeId, selector)
	if errQuery != nil {
		return nil, errQuery
	}

	elements := make([]*Element, len(nodeIds))
	domMap := t.domNodesFromIds(docNode, nodeIds)
	for k, nodeId := range nodeIds {
		elements[k] = newElement(t, domMap[nodeId])
	}
	return elements, nil
}

func (t *Tab) GetDocument() (*gcdapi.DOMNode, error) {
	_, err := t.DOM.RequestChildNodes(0, -1)
	if err != nil {
		return nil, err
	}
	return t.DOM.GetDocument()
}

// Gets all frame ids and urls from the top level document.
func (t *Tab) GetFrameResources() (map[string]string, error) {
	resources, err := t.Page.GetResourceTree()
	if err != nil {
		return nil, err
	}
	resourceMap := make(map[string]string)
	resourceMap[resources.Frame.Id] = resources.Frame.Url
	recursivelyGetFrameResource(resourceMap, resources)
	return resourceMap, nil
}

// Returns the raw source (non-serialized DOM) of the frame. Unfortunately,
// it does not appear possible to get a serialized version using chrome debugger.
// One could extract the urls and load them into a separate tab however.
func (t *Tab) GetFrameSource(id, url string) (string, bool, error) {
	return t.Page.GetResourceContent(id, url)
}

// Returns the outer HTML of the node
func (t *Tab) GetElementSource(id int) (string, error) {
	return t.DOM.GetOuterHTML(id)
}

// Issues a left button mousePressed then mouseReleased on the x, y coords provided.
func (t *Tab) Click(x, y int) error {
	// "mousePressed", "mouseReleased", "mouseMoved"
	// enum": ["none", "left", "middle", "right"]
	pressed := "mousePressed"
	released := "mouseReleased"
	modifiers := 0
	timestamp := 0.0
	button := "left"
	clickCount := 1

	if _, err := t.Input.DispatchMouseEvent(pressed, x, y, modifiers, timestamp, button, clickCount); err != nil {
		return err
	}

	if _, err := t.Input.DispatchMouseEvent(released, x, y, modifiers, timestamp, button, clickCount); err != nil {
		return err
	}
	return nil
}

func (t *Tab) GetElementByNodeId(id int) (*Element, error) {
	domNode, err := t.domNodeFromId(id)
	if err != nil {
		return nil, err
	}
	return newElement(t, domNode), nil
}

// Returns the element by searching the top level document for an element with attributeId
// Does not work on frames.
func (t *Tab) GetElementById(attributeId string) (*Element, error) {
	var err error
	var nodeId int
	var doc *gcdapi.DOMNode
	selector := "#" + attributeId
	doc, err = t.DOM.GetDocument()
	if err != nil {
		return nil, err
	}

	nodeId, err = t.DOM.QuerySelector(doc.NodeId, selector)
	if err != nil {
		return nil, err
	}

	domNode, err := t.findDomNodeInDoc(doc, nodeId)
	if err != nil {
		return nil, err
	}
	return newElement(t, domNode), err
}

func (t *Tab) domNodeFromId(nodeId int) (*gcdapi.DOMNode, error) {
	doc, err := t.DOM.GetDocument()
	if err != nil {
		return nil, err
	}

	return t.findDomNodeInDoc(doc, nodeId)
}

func (t *Tab) findDomNodeInDoc(doc *gcdapi.DOMNode, nodeId int) (*gcdapi.DOMNode, error) {
	// they requested the document
	if nodeId == doc.NodeId {
		return doc, nil
	}
	// requested a child node
	for _, childNode := range doc.Children {
		log.Printf("nodeId: %d childNodeId: %d\n", nodeId, childNode.NodeId)

		if childNode.NodeId == nodeId {
			return childNode, nil
		}
		// recursively check children nodes.
		return t.findDomNodeInDoc(childNode, nodeId)
	}
	return nil, &ElementNotFoundErr{"nodeid doesn't exist"}
}

// loop over child nodes looking for nodeIds
func (t *Tab) domNodesFromIds(doc *gcdapi.DOMNode, nodeIds []int) map[int]*gcdapi.DOMNode {
	domMap := make(map[int]*gcdapi.DOMNode, len(nodeIds))

	for _, childNode := range doc.Children {
		for _, nodeId := range nodeIds {
			if childNode.NodeId == nodeId {
				domMap[nodeId] = childNode
			}
		}
	}
	return domMap
}

//
func (t *Tab) defaultConsoleMessageAdded(fn ConsoleMessageFunc) GcdResponseFunc {
	return func(target *gcd.ChromeTarget, payload []byte) {
		message := &gcdapi.ConsoleConsoleMessage{}
		consoleMessage := &ConsoleEventHeader{}
		err := json.Unmarshal(payload, consoleMessage)
		if err == nil {
			message = consoleMessage.Params.Message
		}
		// call the callback handler
		fn(t, message)
	}
}

func (t *Tab) defaultLoadFired(resp chan<- int) GcdResponseFunc {
	return func(target *gcd.ChromeTarget, payload []byte) {
		target.Unsubscribe("Page.loadEventFired")
		fired := &PageLoadEventFired{}
		err := json.Unmarshal(payload, fired)
		if err != nil {
			resp <- -1
		}
		resp <- fired.timestamp
		close(resp)
	}
}

func recursivelyGetFrameResource(resourceMap map[string]string, resource *gcdapi.PageFrameResourceTree) {
	for _, frame := range resource.ChildFrames {
		resourceMap[frame.Frame.Id] = frame.Frame.Url
		recursivelyGetFrameResource(resourceMap, frame)
	}
}
