# Contributing

Thanks for helping with Java4u. The project is early, so focused changes and clear issue reports are especially useful.

## Before starting

- Open an issue for large changes so the direction can be discussed first.
- Keep Java4u (the launcher) separate from Chunky (the planned mod loader).
- Do not include Minecraft game files, account credentials, or private tokens in commits.
- Preserve the rule that Java4u never asks for a Microsoft account password.

## Build

The current native app targets Windows and .NET 10. From the repository root:

```powershell
dotnet build desktop/Java4u.csproj
```

The browser layout preview is plain HTML, CSS, and JavaScript and has no package installation step.

## Pull requests

- Explain what changed and why.
- Keep pull requests focused and include screenshots for visual changes when helpful.
- Describe any compatibility or data migration impact.
