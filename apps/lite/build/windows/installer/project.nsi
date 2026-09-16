Unicode true

####
## Mizan Lite's Windows installer (L8 D-L8.11).
##
## Three things it does that Wails's scaffold does not:
##   1. installs the WebView2 runtime FROM A BUNDLED COPY, so a shop with no internet still gets a window that paints;
##   2. refuses to install while Mizan Lite is running, so it never replaces a binary whose database is open;
##   3. leaves the shop's data alone when uninstalling — the books outlive the program.
##
## Build it with scripts/lite-package-windows.sh, which checks the bundled runtime is the real one first.
####
!include "wails_tools.nsh"

# ── WebView2, installed from a bundled runtime with no network ────────────────────────────────────
#
# `wails.webview2runtime` embeds the ~1.7MB BOOTSTRAPPER, which fetches the runtime from Microsoft at INSTALL time. On an
# air-gapped shop computer that fails, the application installs, starts, and shows a blank window. So the offline runtime lives
# outside anything Wails manages (build/windows/webview2, shared with Mizan) and this macro installs from it.
!macro lite.webview2offline
    SetRegView 64

    ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
    ${If} $0 != ""
        DetailPrint "WebView2 runtime: already installed"
        Goto webview2_done
    ${EndIf}

    ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
    ${If} $0 != ""
        DetailPrint "WebView2 runtime: already installed for this user"
        Goto webview2_done
    ${EndIf}

    SetDetailsPrint both
    DetailPrint "Installing the WebView2 runtime (no internet required)"
    SetDetailsPrint listonly

    InitPluginsDir
    CreateDirectory "$pluginsdir\webview2offline"
    SetOutPath "$pluginsdir\webview2offline"
    File "..\..\..\..\..\build\windows\webview2\MicrosoftEdgeWebView2RuntimeInstallerX64.exe"

    ExecWait '"$pluginsdir\webview2offline\MicrosoftEdgeWebView2RuntimeInstallerX64.exe" /silent /install' $1

    ${If} $1 != 0
        # Some builds of the runtime installer return non-zero for "already up to date": verify rather than trust the code.
        ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
        ${If} $0 == ""
            ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
        ${EndIf}
        ${If} $0 == ""
            MessageBox MB_OK|MB_ICONSTOP "Mizan Lite could not install the WebView2 runtime it needs to show its window.$\r$\n$\r$\nInstaller exit code: $1$\r$\n$\r$\nMizan Lite has not been installed."
            Abort "WebView2 runtime installation failed (code $1)"
        ${EndIf}
    ${EndIf}

    DetailPrint "WebView2 runtime: installed"
    SetDetailsPrint both

    webview2_done:
!macroend

# ── Never replace a running application ───────────────────────────────────────────────────────────
#
# Mizan Lite holds its SQLite database open while it runs, and on Windows an open file cannot be replaced. The application's
# single-instance lock is a named mutex (apps/lite/options.go: the bundle id, as Wails names it); if it exists, the shop is
# trading. Installing over it would leave a half-written program and a database nobody closed.
!macro lite.refuseWhileRunning
    check_running:
    System::Call 'kernel32::OpenMutexW(i 0x00100000, i 0, w "com.mizanerp.litesim") i .r0'
    ${If} $0 <> 0
        System::Call 'kernel32::CloseHandle(i r0)'
        MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "Mizan Lite is running. Close it (the window's X), then press Retry.$\r$\n$\r$\nإغلاق ميزان لايت أولاً، ثم اضغط إعادة المحاولة." IDRETRY check_running
        Abort "Mizan Lite is running"
    ${EndIf}
!macroend

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_INSTFILES

# Arabic first — the shop's language (Q-L8.7); English for whoever installs it.
!insertmacro MUI_LANGUAGE "Arabic"
!insertmacro MUI_LANGUAGE "English"
!define MUI_LANGDLL_ALLLANGUAGES
!insertmacro MUI_RESERVEFILE_LANGDLL

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
ShowInstDetails show

Function .onInit
   !insertmacro wails.checkArchitecture
   !insertmacro MUI_LANGDLL_DISPLAY
FunctionEnd

Section
    !insertmacro lite.refuseWhileRunning
    !insertmacro wails.setShellContext
    !insertmacro lite.webview2offline

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
SectionEnd

Section "uninstall"
    !insertmacro lite.refuseWhileRunning
    !insertmacro wails.setShellContext

    # The shop's data — its database, its backups, its logs — lives in %LOCALAPPDATA%\Mizan Lite and is NOT touched here.
    # An uninstaller that deletes a shop's books is unrecoverable; reinstalling finds the books where they were.
    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller

    MessageBox MB_OK "Mizan Lite has been removed. Your shop's data is still on this computer:$\r$\n$LOCALAPPDATA\Mizan Lite$\r$\n$\r$\nحُذف البرنامج، وبيانات المحل ما تزال في هذا المجلد."
SectionEnd
