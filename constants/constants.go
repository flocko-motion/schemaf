package constants

// projectName is set by the generated constants.gen.go init() function.
var projectName string

// SetProjectName is called from the generated constants.gen.go to register
// the project name at init time.
func SetProjectName(name string) {
	projectName = name
}

// ProjectName returns the registered project name.
func ProjectName() string {
	if projectName == "" {
		panic("project name not set: did you forget to run codegen?")
	}
	return projectName
}
