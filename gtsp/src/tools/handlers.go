package tools

import (
	"gTSP/src/api"
)

// RegisterAll registers all file system, search, and process handlers to the dispatcher
func RegisterAll(d *api.Dispatcher) {
	d.RegisterWithSchema("list_dir", ListDirHandler, ListDirSchema)
	d.RegisterWithSchema("read_file", ReadFileHandler, ReadFileSchema)
	d.RegisterWithSchema("write_file", WriteFileHandler, WriteFileSchema)
	d.RegisterWithSchema("execute_bash", ExecuteBashHandler, ExecuteBashSchema)
	d.RegisterWithSchema("edit", EditHandler, EditSchema)
	d.RegisterWithSchema("grep_search", GrepSearchHandler, GrepSearchSchema)
	d.RegisterWithSchema("glob", GlobHandler, GlobSchema)
	d.RegisterWithSchema("process_output", ProcessOutputHandler, ProcessOutputSchema)
	d.RegisterWithSchema("process_stop", ProcessStopHandler, ProcessStopSchema)
	d.RegisterWithSchema("process_list", ProcessListHandler, ProcessListSchema)
}
