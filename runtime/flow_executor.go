package runtime

import (
	"bytes"
	"fmt"
	"github.com/faasflow/runtime"
	"github.com/s8sg/goflow/flow"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	sdk "github.com/faasflow/sdk"
	"github.com/faasflow/sdk/executor"
	"github.com/s8sg/goflow/eventhandler"
)

// A signature of SHA265 equivalent of github.com/s8sg/faas-flow
const defaultHmacKey = "71F1D3011F8E6160813B4997BA29856744375A7F26D427D491E1CCABD4627E7C"

type FlowExecutor struct {
	gateway                 string
	flowName                string // the name of the function
	reqID                   string // the request id
	CallbackURL             string // the callback url
	RequestAuthSharedSecret string
	RequestAuthEnabled      bool
	partialState            []byte
	rawRequest              *executor.RawRequest
	StateStore              sdk.StateStore
	DataStore               sdk.DataStore
	EventHandler            sdk.EventHandler
	Logger                  sdk.Logger
	Handler                 FlowDefinitionHandler
	Runtime                 *FlowRuntime
}

type FlowDefinitionHandler func(flow *flow.Workflow, context *flow.Context) error

func (fe *FlowExecutor) HandleNextNode(partial *executor.PartialState) error {
	var err error

	request := &runtime.Request{}
	request.Body, err = partial.Encode()
	if err != nil {
		return fmt.Errorf("failed to encode partial state, error %v", err)
	}
	request.RequestID = fe.reqID
	request.FlowName = fe.flowName

	if fe.MonitoringEnabled() {
		faasHandler := fe.EventHandler.(*eventhandler.FaasEventHandler)
		faasHandler.Tracer.ExtendReqSpan(fe.reqID, faasHandler.CurrentNodeID, "", request)
	}

	err = fe.Runtime.EnqueuePartialRequest(request)
	if err != nil {
		return fmt.Errorf("failed to enqueue request, error %v", err)
	}
	return nil
}

func (fe *FlowExecutor) GetExecutionOption(operation sdk.Operation) map[string]interface{} {
	options := make(map[string]interface{})
	options["gateway"] = fe.gateway
	options["request-id"] = fe.reqID

	return options
}

func (fe *FlowExecutor) HandleExecutionCompletion(data []byte) error {
	if fe.CallbackURL == "" {
		return nil
	}

	log.Printf("calling callback url (%s) with result", fe.CallbackURL)
	httpreq, _ := http.NewRequest(http.MethodPost, fe.CallbackURL, bytes.NewReader(data))
	httpreq.Header.Add("X-Faas-Flow-ReqiD", fe.reqID)
	client := &http.Client{}

	res, resErr := client.Do(httpreq)
	if resErr != nil {
		return resErr
	}
	defer res.Body.Close()
	resData, _ := ioutil.ReadAll(res.Body)

	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
		return fmt.Errorf("failed to call callback %d: %s", res.StatusCode, string(resData))
	}

	return nil
}

func (fe *FlowExecutor) Configure(requestID string) {
	fe.reqID = requestID
}

func (fe *FlowExecutor) GetFlowName() string {
	return fe.flowName
}

func (fe *FlowExecutor) GetFlowDefinition(pipeline *sdk.Pipeline, context *sdk.Context) error {
	workflow := flow.GetWorkflow(pipeline)
	faasflowContext := (*flow.Context)(context)
	return fe.Handler(workflow, faasflowContext)
}

func (fe *FlowExecutor) ReqValidationEnabled() bool {
	return false
}

func (fe *FlowExecutor) GetValidationKey() (string, error) {
	return "", nil
}

func (fe *FlowExecutor) ReqAuthEnabled() bool {
	return fe.RequestAuthEnabled
}

func (fe *FlowExecutor) GetReqAuthKey() (string, error) {
	return fe.RequestAuthSharedSecret, nil
}

func (fe *FlowExecutor) MonitoringEnabled() bool {
	tracing := os.Getenv("enable_tracing")
	if strings.ToUpper(tracing) == "TRUE" {
		return true
	}
	return false
}

func (fe *FlowExecutor) GetEventHandler() (sdk.EventHandler, error) {
	return fe.EventHandler, nil
}

func (fe *FlowExecutor) LoggingEnabled() bool {
	return true
}

func (fe *FlowExecutor) GetLogger() (sdk.Logger, error) {
	return fe.Logger, nil
}

func (fe *FlowExecutor) GetStateStore() (sdk.StateStore, error) {
	return fe.StateStore, nil
}

func (fe *FlowExecutor) GetDataStore() (sdk.DataStore, error) {
	return fe.DataStore, nil
}

func (fe *FlowExecutor) Init(request *runtime.Request) error {
	fe.flowName = request.FlowName

	callbackURL := request.GetHeader("X-Faas-Flow-Callback-Url")
	fe.CallbackURL = callbackURL

	faasHandler := fe.EventHandler.(*eventhandler.FaasEventHandler)
	faasHandler.Header = request.Header

	return nil
}
