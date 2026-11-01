package main

import "errors"

// The legacy saved plan has only RunTasks (full chain), not a module selector.
// Never reinterpret an explicitly selected observation test as consumptions.
func validateSavedTaskMode(runTasks bool, selection int) error {
	if runTasks && selection != 0 {
		return errors.New("单项测试不能保存为完整流程自动启动；请手动执行单项，或取消任务勾选后只保存自动登录")
	}
	return nil
}
