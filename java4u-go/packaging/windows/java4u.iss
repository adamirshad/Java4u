; Java4U Windows installer (Inno Setup 6).
; Built by .github/workflows/release.yml:
;   ISCC /DAppVersion=1.2.3 /DArch=amd64 packaging\windows\java4u.iss
; It installs for the current user only (no admin prompt), adds a Start menu shortcut
; and an uninstaller. Uninstalling keeps the data folder (%APPDATA%\Java4U), so worlds,
; mods and accounts survive a reinstall.

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef Arch
  #define Arch "amd64"
#endif
#define SourceDir "..\..\dist\java4u-windows-" + Arch

[Setup]
AppId={{8F4C6E2A-5B1D-4C3E-9A7F-2D6B8E1C4A90}
AppName=Java4U
AppVersion={#AppVersion}
AppVerName=Java4U {#AppVersion}
AppPublisher=Java4U
AppPublisherURL=https://github.com/adamirshad/Java4u
AppSupportURL=https://github.com/adamirshad/Java4u/issues
AppUpdatesURL=https://github.com/adamirshad/Java4u/releases
DefaultDirName={localappdata}\Programs\Java4U
DefaultGroupName=Java4U
DisableProgramGroupPage=yes
DisableDirPage=auto
PrivilegesRequired=lowest
OutputDir=..\..\dist
OutputBaseFilename=java4u-setup-windows-{#Arch}
SetupIconFile=..\..\build\java4u.ico
UninstallDisplayIcon={app}\java4u-gui.exe
UninstallDisplayName=Java4U
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
#if Arch == "arm64"
ArchitecturesAllowed=arm64
ArchitecturesInstallIn64BitMode=arm64
#else
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
#endif

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#SourceDir}\java4u-gui.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\java4u.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\README.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Java4U"; Filename: "{app}\java4u-gui.exe"; Comment: "Minecraft: Java Edition launcher"
Name: "{autodesktop}\Java4U"; Filename: "{app}\java4u-gui.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\java4u-gui.exe"; Description: "{cm:LaunchProgram,Java4U}"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
; Old copies left by Java4U's own updater.
Type: files; Name: "{app}\*.old"
