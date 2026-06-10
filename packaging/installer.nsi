; Polka installer for Windows (NSIS).
; Build: makensis -DVERSION=x.y.z packaging/installer.nsi
!ifndef VERSION
  !define VERSION "0.0.0"
!endif

Unicode true
Name "Полка ${VERSION}"
OutFile "..\dist\polka-setup-${VERSION}.exe"
InstallDir "$PROGRAMFILES64\Polka"
InstallDirRegKey HKLM "Software\Polka" "InstallDir"
RequestExecutionLevel admin
SetCompressor /SOLID lzma

Page directory
Page instfiles
UninstPage uninstConfirm
UninstPage instfiles

Section "Полка"
  SetOutPath "$INSTDIR"
  File "..\dist\windows\polka-desktop.exe"
  File "..\dist\windows\polka.exe"
  File "polka.ico"

  WriteRegStr HKLM "Software\Polka" "InstallDir" "$INSTDIR"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateDirectory "$SMPROGRAMS\Полка"
  CreateShortCut "$SMPROGRAMS\Полка\Полка.lnk" "$INSTDIR\polka-desktop.exe" "" "$INSTDIR\polka.ico"
  CreateShortCut "$SMPROGRAMS\Полка\Удалить Полку.lnk" "$INSTDIR\uninstall.exe"
  CreateShortCut "$DESKTOP\Полка.lnk" "$INSTDIR\polka-desktop.exe" "" "$INSTDIR\polka.ico"

  ; Entry in "Add or Remove Programs"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Polka" "DisplayName" "Полка"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Polka" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Polka" "DisplayIcon" "$INSTDIR\polka.ico"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Polka" "UninstallString" "$INSTDIR\uninstall.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Polka" "Publisher" "pposevalnikov"
SectionEnd

Section "Uninstall"
  Delete "$INSTDIR\polka-desktop.exe"
  Delete "$INSTDIR\polka.exe"
  Delete "$INSTDIR\polka.ico"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\Полка\Полка.lnk"
  Delete "$SMPROGRAMS\Полка\Удалить Полку.lnk"
  RMDir "$SMPROGRAMS\Полка"
  Delete "$DESKTOP\Полка.lnk"
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Polka"
  DeleteRegKey HKLM "Software\Polka"
SectionEnd
