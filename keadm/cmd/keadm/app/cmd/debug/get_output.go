package debug

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		metav1.SchemeGroupVersion,
	})

	return restMapper
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
