; Windows 安装包补登记的 AAC / M4A 文件关联。
;
; # 为什么不在 mygo.json 的 fileAssociations 里
;
; mygo.json 的 fileAssociations 是**一份三个平台共用**的：macOS 的
; CFBundleDocumentTypes、Linux 的 desktop entry `MimeType=`、Windows
; 安装包的注册表 `Software\Classes` 都从同一份生成。而 Linux 上**没有**
; AAC 解码器（`extaf` 只在 darwin、`mfadec` 只在 windows）——登记给
; Linux 桌面环境的话，`.m4a` 会出现在本应用的「打开方式」里，用户双击
; 得到的只有「这个格式我们放不了」。
;
; 所以 mygo.json 的 fileAssociations 只留**全平台都放得起**的格式，而
; AAC / M4A 由**各自平台的机制**补回：
;
;   - macOS：`macos.infoPlist.CFBundleDocumentTypes`（那份数组会**整个
;     换掉**框架自动生成的那份，见 mygo `bundle.go` 的 InfoPlist 合并）；
;   - Windows：就是这一份——`windows.installerInclude` 挂进安装器的
;     `customInstall` / `customUnInstall` 宏，写法和框架自己生成的
;     `nsisAssociations` 一模一样（HKCU `Software\Classes\<progID>`）；
;   - Linux：不登记。
;
; # 写法为什么照抄框架那一段
;
; 框架的 `nsisAssociations`（mygo `nsis.go`）对 fileAssociations 里的
; 每个扩展名写四个键：ProgID 的显示名、图标、`shell\open\command`、
; 和 `.ext` 的 `OpenWithProgids`。卸载时对称删 ProgID 和 OpenWithProgids。
; 这里对 m4a / aac 做同一件事——**progID 前缀和框架是同一个**
; （identifier `io.github.shellmonster.biu` 清洗之后），于是「打开方式」
; 菜单里的条目和框架自己登记的那些格式**长得一样**。
;
; ⚠️ **SHChangeNotify 不用这里发**：框架在自己那一份关联写完之后就发了
; 一次（`nsisAssociations` 末尾），那一条在这两个宏**之前**跑——资源
; 管理器那时已经收到通知了。
;
; ⚠️ **「以管理员身份运行」那一档不用管**：框架的 `RequestExecutionLevel`
; 是 `user`，HKCU 本来就是当前用户的 hive——写 HKLM 反而会失败。

!macro pudmusicRegisterAACExt ext
  WriteRegStr HKCU "Software\Classes\io.github.shellmonster.biu.${ext}" "" "音频文件"
  WriteRegStr HKCU "Software\Classes\io.github.shellmonster.biu.${ext}\DefaultIcon" "" "$INSTDIR\布丁音乐.exe,0"
  WriteRegStr HKCU "Software\Classes\io.github.shellmonster.biu.${ext}\shell\open\command" "" '"$INSTDIR\布丁音乐.exe" "%1"'
  WriteRegStr HKCU "Software\Classes\.${ext}\OpenWithProgids" "io.github.shellmonster.biu.${ext}" ""
!macroend

!macro pudmusicUnregisterAACExt ext
  DeleteRegKey HKCU "Software\Classes\io.github.shellmonster.biu.${ext}"
  DeleteRegValue HKCU "Software\Classes\.${ext}\OpenWithProgids" "io.github.shellmonster.biu.${ext}"
!macroend

!macro customInstall
  !insertmacro pudmusicRegisterAACExt "m4a"
  !insertmacro pudmusicRegisterAACExt "aac"
!macroend

!macro customUnInstall
  !insertmacro pudmusicUnregisterAACExt "m4a"
  !insertmacro pudmusicUnregisterAACExt "aac"
!macroend
