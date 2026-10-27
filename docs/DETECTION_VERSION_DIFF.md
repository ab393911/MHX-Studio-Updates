# 雷电自动发现版本差异

|版本|启动自动扫描|管理程序缺失时ADB发现|自动保存有效路径|路径/错误报告|
|---|---|---|---|---|
|3.5.0-preview.7|无，需手动点检测|无|仅手动保存设置|简单日志|
|3.5.0-preview.11|有，空闲时12秒刷新|无，先定位管理程序|自动发现不会单独记忆|简单日志|
|本次修复|有|有：独立adb.exe或本机ADB服务|有：独立发现记录，保留config.json|有：检测详情|

用户报告preview.7在同一电脑未检测到实例，不能把它当作preview.11已在用户电脑验收。旧版曾检测到的五个ADB序列号只能证明当时连接可用，不作为程序中的固定设备列表或端口映射。

D:\LDPlayer14是明确的搜索候选，每次均检查文件，不假定安装仍存在。找到并成功读取管理程序的实例列表后才记忆目录。没有运行实例也会保留已创建实例列表。没有管理程序时使用ADB本地服务协议查询实际设备，或查找独立ADB程序。SDK、PATH、运行进程及安装记录均作为候选来源。

实例编号只有管理程序返回并完成身份匹配后才可用于启动/角色控制。ADB原始序列号不能证明雷电编号；不做5556等端口公式反推。纯ADB发现的未知设备可能不是雷电，界面明确显示编号未核对，不自动控制。

本机ADB客户端仅暴露设备枚举及两种身份读取命令，不启动/重启ADB服务，不扫描猜测端口，不提供原生协议游戏点击。参考AOSP官方[服务协议](https://android.googlesource.com/platform/packages/modules/adb/+/ac561557/SERVICES.TXT)与[客户端/服务端协议](https://android.googlesource.com/platform/packages/modules/adb/+/refs/heads/main/docs/dev/overview.md)。独立adb.exe仍按其正常行为工作。

Windows自动测试分别覆盖管理程序、发现记录恢复和ADB协议备用发现，并明确fixture_only=true、real_ldplayer_tested=false。用户Windows+真实雷电尚待验收。游戏任务、登录状态机和在线更新器未重写；升级测试基线改为真实发布的preview.7。
