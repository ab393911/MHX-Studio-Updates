# 私有截图与公开代码测试

用户要求原图、完整诊断ZIP及设备身份仅在当前私有环境处理，脱敏图必须先预览确认再用于公开CI。

公开代码分支从远端已公开基线独立克隆，未合并包含私人样本的本地提交。此分支不含原图、规则副本或待确认的脱敏图。原始本地开发分支及其提交保留。

## 回放接口

Windows验证器保留可选image-replay-dir，读取本地screenshot.png、recorded-profile.json、expected.json。expected中的kind明确区分private-original、redacted与未标明来源；验证结果不将脱敏图或缺失素材称为原图实测。

通用算法测试通过MHX_REPLAY_FIXTURE_DIR显式读取私有目录。没有设置时，两项记录图测试明确skip，不使用合成图伪装通过。公开CI只运行不含私人数据的代码、合成/CI桌面PNG及其他现有回归。

## 本地脱敏工具

cmd/replayredact只供开发环境使用，不接入MHX Studio GUI、不触碰活动规则或数据库。使用指定矩形作不透明纯黑遮盖；不模糊、不缩放、不重采样。输出文件必须新建，不覆盖原图。

internal/fixtureprivacy验证RGBA8 PNG的尺寸和IHDR格式、遮盖外全部像素、受保护区域、不透明alpha；保留sRGB/sBIT等数字显示元数据，去除文本/EXIF元数据。嵌入ICC配置须另行检查。指定私有profile后，工具自动保护每个标定框和按钮范围，并比较所有页面的完整识别评分；不同则拒绝输出。

预览PNG和审计报告只能写入私有工作目录。像素/评分检查不能代替人工隐私检查；用户确认脱敏预览之前不得提交任何预览素材。对私有原图的Windows GUI验证需要可用且获授权的私有Windows环境，Linux或交叉编译不计通过。
