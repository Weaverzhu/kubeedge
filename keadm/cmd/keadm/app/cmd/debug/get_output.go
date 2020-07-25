package debug

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kubectl/pkg/cmd/get"

	"github.com/kubeedge/kubeedge/edge/pkg/metamanager/dao"
)

const (
	tabwriterMinWidth = 8
	tabwriterWidth    = 8
	tabwriterPadding  = 3
	tabwriterPadChar  = '\t'
	tabwriterFlags    = 0
)

type PodInfo struct {
	name     string
	status   string
	restarts int
	ready    string
	ip       string
	node     string
}

// ============== copy from get.go because some of the functions are not exported

type trackingWriterWrapper struct {
	Delegate io.Writer
	Written  int
}

func (t *trackingWriterWrapper) Write(p []byte) (n int, err error) {
	t.Written += len(p)
	return t.Delegate.Write(p)
}

type separatorWriterWrapper struct {
	Delegate io.Writer
	Ready    bool
}

func (s *separatorWriterWrapper) Write(p []byte) (n int, err error) {
	// If we're about to write non-empty bytes and `s` is ready,
	// we prepend an empty line to `p` and reset `s.Read`.
	if len(p) != 0 && s.Ready {
		fmt.Fprintln(s.Delegate)
		s.Ready = false
	}
	return s.Delegate.Write(p)
}

func (s *separatorWriterWrapper) SetReady(state bool) {
	s.Ready = state
}

// ===========================

func toPrinter(o get.GetOptions, mapping *meta.RESTMapping, outputObjects *bool, withNamespace bool, withKind bool) (printers.ResourcePrinterFunc, error) {
	// make a new copy of current flags / opts before mutating
	printFlags := o.PrintFlags.Copy()

	if mapping != nil {
		// if !cmdSpecifiesOutputFmt(cmd) && o.PrintWithOpenAPICols {
		// 	if apiSchema, err := f.OpenAPISchema(); err == nil {
		// 		printFlags.UseOpenAPIColumns(apiSchema, mapping)
		// 	}
		// }
		printFlags.SetKind(mapping.GroupVersionKind.GroupKind())
	}
	if withNamespace {
		printFlags.EnsureWithNamespace()
	}
	if withKind {
		printFlags.EnsureWithKind()
	}

	printer, err := printFlags.ToPrinter()
	if err != nil {
		return nil, err
	}
	printer, err = printers.NewTypeSetter(scheme.Scheme).WrapToPrinter(printer, nil)
	if err != nil {
		return nil, err
	}

	// if o.Sort {
	// 	printer = &SortingPrinter{Delegate: printer, SortField: sortBy}
	// }
	// if outputObjects != nil {
	// 	printer = &skipPrinter{delegate: printer, output: outputObjects}
	// }
	// if o.ServerPrint {
	// 	printer = &TablePrinter{Delegate: printer}
	// }
	return printer.PrintObj, nil
}

// NewRestMapper returns a default RESTMapper
func NewRestMapper() meta.RESTMapper {
	restMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{
		corev1.SchemeGroupVersion,
	})

	return restMapper
}

func meta2Obj(v *dao.Meta) (*runtime.Object, error) {
	byteJSON := []byte(v.Value)
	jsonMap := make(map[string]interface{})
	err := json.Unmarshal(byteJSON, &jsonMap)
	if err != nil {
		return nil, err
	}

	jsonMap["apiVersion"] = corev1.SchemeGroupVersion.Version
	jsonMap["kind"] = "Pod"
	byteJSON, err = json.Marshal(jsonMap)
	if err != nil {
		return nil, err
	}

	converted, err := runtime.Decode(unstructured.UnstructuredJSONScheme, byteJSON)
	if err != nil {
		return nil, err
	}
	return &converted, nil
}

func PrintWithKubectl(metas *[]dao.Meta, o *get.GetOptions) error {
	// track if we write any output
	trackingWriter := &trackingWriterWrapper{Delegate: o.Out}
	// output an empty line separating output
	separatorWriter := &separatorWriterWrapper{Delegate: trackingWriter}

	w := printers.GetNewTabWriter(separatorWriter)

	mapper := NewRestMapper()

	for _, v := range *metas {
		obj, err := meta2Obj(&v)
		if err != nil {
			return err
		}
		print("before get mapping\n")
		gk := printers.GetObjectGroupKind(*obj)
		mapping, err := mapper.RESTMapping(gk, corev1.SchemeGroupVersion.Version)

		if err != nil {
			return err
		}
		print("after get mapping\n")
		print("before printobj\n")
		localPrinter, err := o.ToPrinter(mapping, nil, false, false)

		localPrinter.PrintObj(*obj, w)
	}
	return nil
}

func NewTabWriter(out io.Writer) *tabwriter.Writer {
	writer := new(tabwriter.Writer)
	writer.Init(out, tabwriterMinWidth, tabwriterWidth, tabwriterPadding, tabwriterPadChar, tabwriterFlags)
	return writer
}

func getReadyAndRestartCount(containerStatuses []interface{}) (int, int) {
	totReadyCount := 0
	totRestartCount := 0
	for _, v := range containerStatuses {
		mapData := v.(map[string]interface{})
		isReady := mapData["ready"].(bool)
		if isReady {
			totReadyCount++
		}

		totRestartCount += int(mapData["restartCount"].(float64))
	}
	return totReadyCount, totRestartCount
}

// MetaToPodOuput convert []dao.Meta to []PodInfo
func MetaToPodInfo(metas *[]dao.Meta) (*[]PodInfo, error) {
	result := make([]PodInfo, 0)
	for _, v := range *metas {
		var metadata map[string]interface{}
		var status map[string]interface{}
		var containerStatuses []interface{}
		var spec map[string]interface{}

		jsonMap := make(map[string]interface{})
		byteJSON := []byte(v.Value)
		err := json.Unmarshal(byteJSON, &jsonMap)
		if err != nil {
			return nil, err
		}

		metadata = jsonMap["metadata"].(map[string]interface{})
		status = jsonMap["status"].(map[string]interface{})
		containerStatuses = status["containerStatuses"].([]interface{})
		spec = jsonMap["spec"].(map[string]interface{})

		readyCount, restartCount := getReadyAndRestartCount(containerStatuses)

		newPodInfo := PodInfo{
			name:     metadata["name"].(string),
			status:   status["phase"].(string),
			restarts: restartCount,
			ready:    fmt.Sprintf("%d/%d", readyCount, len(containerStatuses)),
			ip:       status["podIP"].(string),
			node:     spec["nodeName"].(string),
		}

		result = append(result, newPodInfo)
	}
	return &result, nil
}

func OutputPodInfo(result *[]PodInfo, out io.Writer) {
	writer := NewTabWriter(out)
	defer writer.Flush()
	fmt.Fprintf(writer, "NAME\tSTAUTS\tRESTARTS\tREADY\tIP\tNODE\t")
	for _, v := range *result {
		fmt.Fprintf(writer, "\n%s\t%s\t%d\t%s\t%s\t%s", v.name, v.status, v.restarts, v.ready, v.ip, v.node)
	}
	fmt.Fprintln(writer)
}
