using System.Collections.ObjectModel;
using System.Diagnostics;
using System.IO;
using System.Net.Http;
using System.Text.RegularExpressions;
using System.Windows;
using System.Windows.Controls;

namespace Java4u;

public partial class MainWindow : Window
{
    private readonly InstanceStore _store = new();
    private readonly LauncherSettingsStore _settingsStore = new();
    private readonly MinecraftAuthStore _authStore = new();
    private readonly MinecraftAuthService _authService = new();
    private readonly ObservableCollection<InstanceProfile> _instances = [];
    private readonly HashSet<Process> _runningGameProcesses = [];
    private bool _isInstalling;
    private bool _isInstallingJava;
    private bool _isCheckingJava;
    private bool _isAuthenticating;
    private bool _isLaunching;
    private CancellationTokenSource? _installCancellation;
    private CancellationTokenSource? _javaInstallCancellation;
    private CancellationTokenSource? _authCancellation;
    private MinecraftAuthSession? _account;

    public MainWindow()
    {
        InitializeComponent();
        InstanceList.ItemsSource = _instances;
        Loaded += async (_, _) =>
        {
            await LoadInstancesAsync();
            await LoadAccountAsync();
        };
    }

    private async Task LoadInstancesAsync()
    {
        try
        {
            var knownIds = new HashSet<Guid>();
            foreach (var instance in await _store.LoadAsync())
            {
                if (instance.Id == Guid.Empty || !knownIds.Add(instance.Id))
                {
                    instance.Id = Guid.NewGuid();
                    knownIds.Add(instance.Id);
                }
                _store.PrepareInstanceDirectory(instance);
                _instances.Add(instance);
            }
            await SaveInstancesAsync();
            RefreshState();
        }
        catch (Exception exception)
        {
            MessageBox.Show(this, $"Java4u couldn't read its local instance list.\n\n{exception.Message}", "Library couldn't load", MessageBoxButton.OK, MessageBoxImage.Warning);
            RefreshState();
        }
    }

    private async Task SaveInstancesAsync()
    {
        try { await _store.SaveAsync(_instances.ToArray()); }
        catch (Exception exception)
        {
            MessageBox.Show(this, $"Java4u couldn't save the instance list.\n\n{exception.Message}", "Couldn't save", MessageBoxButton.OK, MessageBoxImage.Error);
        }
    }

    private void RefreshState()
    {
        InstanceList.Items.Refresh();
        InstanceCount.Text = $"{_instances.Count} INSTANCE{(_instances.Count == 1 ? "" : "S")}";
        EmptyState.Visibility = _instances.Count == 0 ? Visibility.Visible : Visibility.Collapsed;
        var selectedProfile = InstanceList.SelectedItem as InstanceProfile;
        var hasSelection = selectedProfile is not null;
        InstallButton.IsEnabled = hasSelection && !_isInstalling && !_isInstallingJava;
        InstallButton.Content = selectedProfile?.IsInstalled == true ? "Repair game files" : "Install game files";
        LaunchButton.IsEnabled = selectedProfile is { IsInstalled: true, Loader: "Vanilla" } && _account is not null && !_isInstalling && !_isInstallingJava && !_isLaunching;
        LaunchButton.ToolTip = selectedProfile?.Loader == "Chunky" ? "Chunky is an early compatibility alpha. Current limits are shown by Inspect mods before launch." : null;
        CheckJavaButton.IsEnabled = selectedProfile is { IsInstalled: true, RequiredJavaMajorVersion: > 0 } && !_isInstalling && !_isInstallingJava && !_isCheckingJava;
        InspectModsButton.IsEnabled = hasSelection && !_isInstalling && !_isInstallingJava;
        OpenFolderButton.IsEnabled = hasSelection;
        EditButton.IsEnabled = hasSelection && !_isInstalling && !_isInstallingJava;
        DeleteButton.IsEnabled = hasSelection && !_isInstalling && !_isInstallingJava;
        InstanceList.IsEnabled = !_isInstalling && !_isInstallingJava;
        SettingsButton.IsEnabled = !_isAuthenticating && !_isLaunching;
        CancelInstallButton.Content = _isInstallingJava ? "Cancel Java setup" : "Cancel install";
        CancelInstallButton.Visibility = _isInstalling || _isInstallingJava ? Visibility.Visible : Visibility.Collapsed;
        SelectedInstancePath.Text = InstanceList.SelectedItem is InstanceProfile selected
            ? selected.InstanceDirectory
            : "Select an instance to see its folder.";

    }

    private async void NewInstanceButton_Click(object sender, RoutedEventArgs e)
    {
        if (_isInstalling) return;
        var form = new CreateInstanceWindow { Owner = this };
        if (form.ShowDialog() != true || form.Result is not { } instance) return;
        try { _store.PrepareInstanceDirectory(instance); }
        catch (Exception exception)
        {
            MessageBox.Show(this, $"Java4u couldn't create this instance's folder.\n\n{exception.Message}", "Couldn't create instance", MessageBoxButton.OK, MessageBoxImage.Error);
            return;
        }
        _instances.Add(instance);
        InstanceList.SelectedItem = instance;
        await SaveInstancesAsync();
        RefreshState();
    }

    private async void EditButton_Click(object sender, RoutedEventArgs e)
    {
        if (InstanceList.SelectedItem is not InstanceProfile selected) return;
        var form = new CreateInstanceWindow(selected) { Owner = this };
        if (form.ShowDialog() != true || form.Result is not { } updated) return;
        _store.PrepareInstanceDirectory(updated);
        var index = _instances.IndexOf(selected);
        if (index < 0) return;
        _instances[index] = updated;
        InstanceList.SelectedItem = updated;
        await SaveInstancesAsync();
        RefreshState();
    }

    private async void DeleteButton_Click(object sender, RoutedEventArgs e)
    {
        if (InstanceList.SelectedItem is not InstanceProfile selected) return;
        var result = MessageBox.Show(this, $"Remove the profile '{selected.Name}' from Java4u? This only removes its launcher profile; no game files or saves are deleted.", "Remove instance profile", MessageBoxButton.YesNo, MessageBoxImage.Question);
        if (result != MessageBoxResult.Yes) return;
        _instances.Remove(selected);
        await SaveInstancesAsync();
        RefreshState();
    }

    private async void InstallButton_Click(object sender, RoutedEventArgs e)
    {
        if (InstanceList.SelectedItem is not InstanceProfile selected) return;
        _isInstalling = true;
        _installCancellation = new CancellationTokenSource();
        OperationStatus.Text = $"Preparing Minecraft {selected.MinecraftVersion}…";
        RefreshState();
        try
        {
            var progress = new Progress<string>(message => OperationStatus.Text = message);
            await new MinecraftCoreInstaller().InstallAsync(selected, progress, _installCancellation.Token);
            await SaveInstancesAsync();
            MessageBox.Show(this, $"Minecraft {selected.MinecraftVersion}'s metadata, client, libraries, assets, and Windows native libraries are ready in this instance. Use Check Java and sign in with a Minecraft Java account before launching.", "Game files downloaded", MessageBoxButton.OK, MessageBoxImage.Information);
        }
        catch (OperationCanceledException)
        {
            OperationStatus.Text = "Install canceled. Verified files can be reused next time.";
        }
        catch (Exception exception)
        {
            OperationStatus.Text = "Download stopped. You can retry when you're online.";
            MessageBox.Show(this, $"Java4u couldn't prepare Minecraft {selected.MinecraftVersion}.\n\n{exception.Message}", "Download failed", MessageBoxButton.OK, MessageBoxImage.Warning);
        }
        finally
        {
            _installCancellation.Dispose();
            _installCancellation = null;
            _isInstalling = false;
            RefreshState();
        }
    }

    private void CancelInstallButton_Click(object sender, RoutedEventArgs e)
    {
        _installCancellation?.Cancel();
        _javaInstallCancellation?.Cancel();
    }

    private async void CheckJavaButton_Click(object sender, RoutedEventArgs e)
    {
        if (InstanceList.SelectedItem is not InstanceProfile { IsInstalled: true, RequiredJavaMajorVersion: > 0 } selected || _isCheckingJava) return;
        _isCheckingJava = true;
        OperationStatus.Text = "Checking the Java runtime…";
        RefreshState();
        try
        {
            JavaRuntimeReport report;
            try { report = await new JavaRuntimeInspector().InspectAsync(selected); }
            catch (FileNotFoundException)
            {
                var choice = MessageBox.Show(this, $"Java {selected.RequiredJavaMajorVersion} wasn't found. Download and configure Eclipse Temurin Java {selected.RequiredJavaMajorVersion} for this instance?", "Java runtime missing", MessageBoxButton.YesNo, MessageBoxImage.Question);
                if (choice != MessageBoxResult.Yes) return;
                report = await InstallJavaRuntimeAsync(selected);
            }
            if (!report.IsCompatible)
            {
                var choice = MessageBox.Show(this, $"{report.Summary}\n\nDownload and configure Eclipse Temurin Java {selected.RequiredJavaMajorVersion} for this instance?", "Java version mismatch", MessageBoxButton.YesNo, MessageBoxImage.Question);
                if (choice != MessageBoxResult.Yes)
                {
                    MessageBox.Show(this, $"{report.Summary}\n\n{report.ExecutablePath}", "Java version mismatch", MessageBoxButton.OK, MessageBoxImage.Warning);
                    return;
                }
                report = await InstallJavaRuntimeAsync(selected);
            }
            selected.JavaExecutablePath = report.ExecutablePath;
            await SaveInstancesAsync();
            OperationStatus.Text = report.Summary;
            MessageBox.Show(this, $"{report.Summary}\n\n{report.ExecutablePath}", report.IsCompatible ? "Java is ready" : "Java version mismatch", MessageBoxButton.OK, report.IsCompatible ? MessageBoxImage.Information : MessageBoxImage.Warning);
        }
        catch (OperationCanceledException)
        {
            OperationStatus.Text = "Java setup canceled. The existing runtime was left unchanged.";
        }
        catch (Exception exception)
        {
            OperationStatus.Text = "Java runtime couldn't be checked.";
            MessageBox.Show(this, exception.Message, "Java runtime check failed", MessageBoxButton.OK, MessageBoxImage.Warning);
        }
        finally
        {
            _isCheckingJava = false;
            RefreshState();
        }
    }

    private async Task<JavaRuntimeReport> InstallJavaRuntimeAsync(InstanceProfile profile)
    {
        _isInstallingJava = true;
        _javaInstallCancellation = new CancellationTokenSource();
        OperationStatus.Text = $"Preparing Java {profile.RequiredJavaMajorVersion}…";
        RefreshState();
        try
        {
            var progress = new Progress<string>(message => OperationStatus.Text = message);
            var javaPath = await new JavaRuntimeManager().InstallTemurinAsync(profile.RequiredJavaMajorVersion, progress, _javaInstallCancellation.Token);
            profile.JavaExecutablePath = javaPath;
            await SaveInstancesAsync();
            return await new JavaRuntimeInspector().InspectAsync(profile);
        }
        finally
        {
            _javaInstallCancellation.Dispose();
            _javaInstallCancellation = null;
            _isInstallingJava = false;
            RefreshState();
        }
    }

    private void OpenFolderButton_Click(object sender, RoutedEventArgs e)
    {
        if (InstanceList.SelectedItem is not InstanceProfile selected) return;
        try
        {
            _store.PrepareInstanceDirectory(selected);
            Process.Start(new ProcessStartInfo { FileName = selected.InstanceDirectory, UseShellExecute = true });
        }
        catch (Exception exception)
        {
            MessageBox.Show(this, $"Java4u couldn't open this instance's folder.\n\n{exception.Message}", "Couldn't open folder", MessageBoxButton.OK, MessageBoxImage.Warning);
        }
    }

    private async void InspectModsButton_Click(object sender, RoutedEventArgs e)
    {
        if (InstanceList.SelectedItem is not InstanceProfile profile || _isInstalling) return;
        InspectModsButton.IsEnabled = false;
        OperationStatus.Text = "Inspecting Fabric mod metadata…";
        try
        {
            var report = await new ChunkyModInspector().InspectAsync(profile);
            OperationStatus.Text = $"Found {report.Mods.Count} Fabric metadata file(s), with {report.Issues.Count} issue(s).";
            var details = report.Mods.Count == 0
                ? "No Fabric mods were detected in this instance's mods folder."
                : string.Join(Environment.NewLine, report.Mods.Select(mod => $"{mod.Id} {mod.Version} · {mod.Environment}"));
            if (report.Issues.Count > 0)
                details += Environment.NewLine + Environment.NewLine + string.Join(Environment.NewLine, report.Issues.Take(12).Select(issue => $"{issue.FileName}: {issue.Message}"));
            MessageBox.Show(this, details + Environment.NewLine + Environment.NewLine + "Chunky alpha can load simple main and client initializers. Mixins, access wideners, nested jars, Fabric API, Fabric Loader services, and game lifecycle hooks are not implemented yet. Mods with reported issues are blocked from launch.", "Chunky alpha compatibility", MessageBoxButton.OK, report.Issues.Count == 0 ? MessageBoxImage.Information : MessageBoxImage.Warning);
        }
        catch (Exception exception)
        {
            OperationStatus.Text = "Mod inspection failed.";
            MessageBox.Show(this, exception.Message, "Couldn't inspect mods", MessageBoxButton.OK, MessageBoxImage.Warning);
        }
        finally { RefreshState(); }
    }

    private void InstanceList_SelectionChanged(object sender, SelectionChangedEventArgs e) => RefreshState();

    private void InstancesButton_Click(object sender, RoutedEventArgs e) => InstanceList.Focus();
    private void ModsButton_Click(object sender, RoutedEventArgs e)
    {
        if (InstanceList.SelectedItem is not InstanceProfile selected)
        {
            MessageBox.Show(this, "Select an instance first so Java4u can find compatible Fabric mods for its Minecraft version.", "Choose an instance", MessageBoxButton.OK, MessageBoxImage.Information);
            return;
        }
        new ModBrowserWindow(selected) { Owner = this }.ShowDialog();
    }

    private void DownloadsButton_Click(object sender, RoutedEventArgs e)
    {
        if (InstanceList.SelectedItem is not InstanceProfile selected)
        {
            MessageBox.Show(this, "Select an instance first to manage its Minecraft and mod downloads.", "Choose an instance", MessageBoxButton.OK, MessageBoxImage.Information);
            return;
        }
        var center = new DownloadCenterWindow(selected, OperationStatus.Text) { Owner = this };
        if (center.ShowDialog() != true) return;
        switch (center.Action)
        {
            case DownloadCenterAction.InstallGame:
                InstallButton_Click(this, new RoutedEventArgs());
                break;
            case DownloadCenterAction.ModBrowser:
                ModsButton_Click(this, new RoutedEventArgs());
                break;
            case DownloadCenterAction.OpenFolder:
                OpenFolderButton_Click(this, new RoutedEventArgs());
                break;
        }
    }
    private async void SettingsButton_Click(object sender, RoutedEventArgs e)
    {
        var settings = await _settingsStore.LoadAsync();
        var window = new LauncherSettingsWindow(settings) { Owner = this };
        if (window.ShowDialog() != true || window.Result is not { } updated) return;
        if (_account is not null && !string.Equals(settings.MicrosoftClientId, updated.MicrosoftClientId, StringComparison.OrdinalIgnoreCase))
        {
            var choice = MessageBox.Show(this, "Changing Java4u's Microsoft client ID requires a new sign-in. Remove the currently saved account session and continue?", "Sign in again required", MessageBoxButton.YesNo, MessageBoxImage.Question);
            if (choice != MessageBoxResult.Yes) return;
            _authStore.Delete();
            _account = null;
            UpdateAccountButton();
        }
        await _settingsStore.SaveAsync(updated);
        RefreshState();
    }
    private void HelpButton_Click(object sender, RoutedEventArgs e) => ShowPlannedFeature("Getting started with Java4u", "1. Create an instance and install its game files.\n2. Choose Check Java; Java4u can install a matching Temurin runtime. Chunky alpha requires Java 17 or newer.\n3. Add Java4u's own Microsoft public-client ID in Settings, then sign in with the Minecraft Java account.\n4. Inspect mods to see whether Chunky alpha supports the installed mod set, then launch.\n\nChunky alpha currently supports simple main/client entrypoints only. Mixins, Fabric API, Fabric Loader services, nested jars, access wideners, and Minecraft lifecycle hooks remain unsupported.");

    private void ShowPlannedFeature(string title, string body) => MessageBox.Show(this, body, title, MessageBoxButton.OK, MessageBoxImage.Information);

    private async Task LoadAccountAsync()
    {
        try
        {
            _account = await _authStore.LoadAsync();
            UpdateAccountButton();
            RefreshState();
        }
        catch (Exception exception)
        {
            MessageBox.Show(this, $"Java4u couldn't unlock the saved Microsoft account for this Windows user. You can sign in again.\n\n{exception.Message}", "Saved sign-in unavailable", MessageBoxButton.OK, MessageBoxImage.Warning);
            _account = null;
            UpdateAccountButton();
            RefreshState();
        }
    }

    private async void AccountButton_Click(object sender, RoutedEventArgs e)
    {
        if (_isAuthenticating) return;
        if (_account is not null)
        {
            if (MessageBox.Show(this, $"Sign out { _account.PlayerName } from Java4u? The encrypted saved sign-in will be removed.", "Sign out", MessageBoxButton.YesNo, MessageBoxImage.Question) != MessageBoxResult.Yes) return;
            _authStore.Delete();
            _account = null;
            UpdateAccountButton();
            RefreshState();
            return;
        }

        try
        {
            var settings = await _settingsStore.LoadAsync();
            if (string.IsNullOrWhiteSpace(settings.MicrosoftClientId))
            {
                var settingsWindow = new LauncherSettingsWindow(settings) { Owner = this };
                if (settingsWindow.ShowDialog() != true || settingsWindow.Result is not { } updated) return;
                settings = updated;
                await _settingsStore.SaveAsync(settings);
            }

            var signInRequest = await _authService.BeginSignInAsync(settings.MicrosoftClientId);
            var prompt = new DeviceSignInWindow(signInRequest) { Owner = this };
            if (prompt.ShowDialog() != true) return;

            _isAuthenticating = true;
            _authCancellation = new CancellationTokenSource();
            AccountButton.IsEnabled = false;
            CancelAuthButton.Visibility = Visibility.Visible;
            OperationStatus.Text = "Waiting for Microsoft sign-in…";
            var progress = new Progress<AuthProgress>(item => OperationStatus.Text = item.Message);
            var session = await _authService.CompleteSignInAsync(settings.MicrosoftClientId, signInRequest, progress, _authCancellation.Token);
            await _authStore.SaveAsync(session);
            _account = session;
            OperationStatus.Text = $"Signed in as {session.PlayerName}.";
            UpdateAccountButton();
            MessageBox.Show(this, $"Signed in as {session.PlayerName}. Java4u confirmed Minecraft: Java Edition access.", "Microsoft account connected", MessageBoxButton.OK, MessageBoxImage.Information);
        }
        catch (OperationCanceledException)
        {
            OperationStatus.Text = "Microsoft sign-in canceled.";
        }
        catch (Exception exception)
        {
            OperationStatus.Text = "Microsoft sign-in didn't finish.";
            MessageBox.Show(this, exception.Message, "Sign-in failed", MessageBoxButton.OK, MessageBoxImage.Warning);
        }
        finally
        {
            _authCancellation?.Dispose();
            _authCancellation = null;
            CancelAuthButton.Visibility = Visibility.Collapsed;
            _isAuthenticating = false;
            AccountButton.IsEnabled = true;
            UpdateAccountButton();
            RefreshState();
        }
    }

    private void UpdateAccountButton()
    {
        AccountButton.Content = _account is null ? "Sign in with Microsoft" : $"Sign out · {_account.PlayerName}";
    }

    private void CancelAuthButton_Click(object sender, RoutedEventArgs e) => _authCancellation?.Cancel();

    private async void LaunchButton_Click(object sender, RoutedEventArgs e)
    {
        if (_isLaunching || InstanceList.SelectedItem is not InstanceProfile selected || _account is null) return;
        _isLaunching = true;
        AccountButton.IsEnabled = false;
        OperationStatus.Text = $"Preparing Minecraft {selected.MinecraftVersion}…";
        RefreshState();
        try
        {
            var settings = await _settingsStore.LoadAsync();
            var session = _account!;
            if (session.ExpiresUtc <= DateTimeOffset.UtcNow.AddMinutes(2))
            {
                OperationStatus.Text = "Refreshing the Microsoft sign-in…";
                try { session = await _authService.RefreshAndValidateAsync(settings.MicrosoftClientId, session.MicrosoftRefreshToken); }
                catch (MinecraftReauthenticationRequiredException)
                {
                    _authStore.Delete();
                    _account = null;
                    UpdateAccountButton();
                    throw;
                }
                await _authStore.SaveAsync(session);
                _account = session;
                UpdateAccountButton();
            }

            var process = await new MinecraftLaunchService().LaunchAsync(selected, session, settings.MicrosoftClientId);
            var versionId = selected.MinecraftVersion;
            _runningGameProcesses.Add(process);
            process.Exited += (_, _) => Dispatcher.InvokeAsync(() =>
            {
                _runningGameProcesses.Remove(process);
                OperationStatus.Text = $"Minecraft {versionId} closed.";
                process.Dispose();
            });
            if (process.HasExited)
            {
                _runningGameProcesses.Remove(process);
                process.Dispose();
                OperationStatus.Text = $"Minecraft {versionId} closed.";
            }
            else OperationStatus.Text = $"Minecraft {versionId} is running.";
        }
        catch (Exception exception)
        {
            OperationStatus.Text = "Minecraft couldn't start.";
            MessageBox.Show(this, exception.Message, "Launch failed", MessageBoxButton.OK, MessageBoxImage.Warning);
        }
        finally
        {
            _isLaunching = false;
            AccountButton.IsEnabled = !_isAuthenticating;
            RefreshState();
        }
    }
}

public sealed class LauncherSettingsWindow : Window
{
    private readonly TextBox _clientIdBox = new();
    public LauncherSettings? Result { get; private set; }

    public LauncherSettingsWindow(LauncherSettings current)
    {
        Title = "Java4u settings";
        Width = 540;
        Height = 390;
        ResizeMode = ResizeMode.NoResize;
        WindowStartupLocation = WindowStartupLocation.CenterOwner;
        Background = System.Windows.Media.Brushes.Transparent;
        AllowsTransparency = true;
        WindowStyle = WindowStyle.None;
        ShowInTaskbar = false;

        var panel = new StackPanel { Margin = new Thickness(26) };
        Content = new Border
        {
            Background = (System.Windows.Media.Brush)Application.Current.Resources["PanelBg"],
            BorderBrush = (System.Windows.Media.Brush)Application.Current.Resources["PanelBorder"],
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(10),
            Child = panel,
        };
        panel.Children.Add(new TextBlock { Text = "JAVA4U SETTINGS", Foreground = (System.Windows.Media.Brush)Application.Current.Resources["Accent"], FontFamily = new System.Windows.Media.FontFamily("Consolas"), FontSize = 10, Margin = new Thickness(0, 2, 0, 8) });
        panel.Children.Add(new TextBlock { Text = "Microsoft sign-in", FontSize = 23, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 0, 0, 18) });
        panel.Children.Add(new TextBlock { Text = "JAVA4U MICROSOFT APP CLIENT ID", Foreground = (System.Windows.Media.Brush)Application.Current.Resources["MutedText"], FontFamily = new System.Windows.Media.FontFamily("Consolas"), FontSize = 9, Margin = new Thickness(0, 4, 0, 6) });
        _clientIdBox.Text = current.MicrosoftClientId;
        _clientIdBox.Margin = new Thickness(0, 0, 0, 12);
        panel.Children.Add(_clientIdBox);
        panel.Children.Add(new TextBlock
        {
            Text = "Use the Application (client) ID from Java4u's own Microsoft Entra public-client app registration. Do not enter a client secret. Microsoft sign-in uses a device code in your browser. Minecraft Java services may also require Mojang to approve the app registration before account exchange is allowed.",
            Foreground = (System.Windows.Media.Brush)Application.Current.Resources["MutedText"],
            FontSize = 11,
            TextWrapping = TextWrapping.Wrap,
            Margin = new Thickness(0, 3, 0, 16),
        });
        var actions = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right };
        var cancel = new Button { Content = "Cancel", Margin = new Thickness(0, 0, 8, 0), MinWidth = 80 };
        cancel.Click += (_, _) => { DialogResult = false; Close(); };
        var save = new Button { Content = "Save", MinWidth = 90, Background = (System.Windows.Media.Brush)Application.Current.Resources["Accent"], Foreground = (System.Windows.Media.Brush)Application.Current.Resources["WindowBg"] };
        save.Click += Save_Click;
        actions.Children.Add(cancel);
        actions.Children.Add(save);
        panel.Children.Add(actions);
        _clientIdBox.Focus();
    }

    private void Save_Click(object sender, RoutedEventArgs e)
    {
        var clientId = _clientIdBox.Text.Trim();
        if (clientId.Length > 0 && !Guid.TryParse(clientId, out _))
        {
            MessageBox.Show(this, "Enter the GUID Application (client) ID, or leave the field blank to disable Microsoft sign-in.", "Check client ID", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }
        Result = new LauncherSettings { MicrosoftClientId = clientId };
        DialogResult = true;
        Close();
    }
}

public sealed class DeviceSignInWindow : Window
{
    public DeviceSignInWindow(DeviceSignInRequest request)
    {
        Title = "Sign in with Microsoft";
        Width = 460;
        Height = 330;
        ResizeMode = ResizeMode.NoResize;
        WindowStartupLocation = WindowStartupLocation.CenterOwner;
        Background = System.Windows.Media.Brushes.Transparent;
        AllowsTransparency = true;
        WindowStyle = WindowStyle.None;
        ShowInTaskbar = false;

        var panel = new StackPanel { Margin = new Thickness(26) };
        Content = new Border
        {
            Background = (System.Windows.Media.Brush)Application.Current.Resources["PanelBg"],
            BorderBrush = (System.Windows.Media.Brush)Application.Current.Resources["PanelBorder"],
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(10),
            Child = panel,
        };
        panel.Children.Add(new TextBlock { Text = "MICROSOFT ACCOUNT", Foreground = (System.Windows.Media.Brush)Application.Current.Resources["Accent"], FontFamily = new System.Windows.Media.FontFamily("Consolas"), FontSize = 10, Margin = new Thickness(0, 2, 0, 8) });
        panel.Children.Add(new TextBlock { Text = "Continue in your browser", FontSize = 21, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 0, 0, 8) });
        panel.Children.Add(new TextBlock { Text = $"Open {request.VerificationUri} and enter this one-time code:", Foreground = (System.Windows.Media.Brush)Application.Current.Resources["MutedText"], FontSize = 11, TextWrapping = TextWrapping.Wrap });
        var code = new TextBox { Text = request.UserCode, IsReadOnly = true, FontFamily = new System.Windows.Media.FontFamily("Consolas"), FontSize = 22, HorizontalContentAlignment = HorizontalAlignment.Center, Margin = new Thickness(0, 12, 0, 12) };
        panel.Children.Add(code);
        var actions = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right };
        var cancel = new Button { Content = "Cancel", Margin = new Thickness(0, 0, 8, 0), MinWidth = 80 };
        cancel.Click += (_, _) => { DialogResult = false; Close(); };
        var copy = new Button { Content = "Copy code", Margin = new Thickness(0, 0, 8, 0), MinWidth = 90 };
        copy.Click += (_, _) => Clipboard.SetText(request.UserCode);
        var open = new Button { Content = "Open sign-in page", Margin = new Thickness(0, 0, 8, 0), MinWidth = 125 };
        open.Click += (_, _) => Process.Start(new ProcessStartInfo { FileName = request.VerificationUri, UseShellExecute = true });
        var continueButton = new Button { Content = "I've signed in", MinWidth = 105, Background = (System.Windows.Media.Brush)Application.Current.Resources["Accent"], Foreground = (System.Windows.Media.Brush)Application.Current.Resources["WindowBg"] };
        continueButton.Click += (_, _) => { DialogResult = true; Close(); };
        actions.Children.Add(cancel);
        actions.Children.Add(copy);
        actions.Children.Add(open);
        actions.Children.Add(continueButton);
        panel.Children.Add(actions);
        panel.Children.Add(new TextBlock { Text = $"The code expires in about {Math.Max(1, request.ExpiresInSeconds / 60)} minutes. Java4u never asks for your Microsoft password.", Foreground = (System.Windows.Media.Brush)Application.Current.Resources["MutedText"], FontSize = 10, TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 13, 0, 0) });
    }
}

public sealed class CreateInstanceWindow : Window
{
    private readonly InstanceProfile? _original;
    private readonly TextBox _nameBox = new();
    private readonly ComboBox _versionBox = new();
    private readonly TextBlock _versionStatus = new();
    private readonly TextBox _memoryBox = new();
    private readonly TextBox _javaPathBox = new();
    private readonly ComboBox _loaderBox = new();
    public InstanceProfile? Result { get; private set; }

    public CreateInstanceWindow(InstanceProfile? existing = null)
    {
        _original = existing;
        Title = existing is null ? "New instance" : "Instance settings";
        Width = 420;
        Height = 580;
        ResizeMode = ResizeMode.NoResize;
        WindowStartupLocation = WindowStartupLocation.CenterOwner;
        Background = System.Windows.Media.Brushes.Transparent;
        AllowsTransparency = true;
        WindowStyle = WindowStyle.None;
        ShowInTaskbar = false;

        var panel = new StackPanel { Margin = new Thickness(26) };
        var border = new Border { Background = (System.Windows.Media.Brush)Application.Current.Resources["PanelBg"], BorderBrush = (System.Windows.Media.Brush)Application.Current.Resources["PanelBorder"], BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(10), Child = panel };
        Content = border;
        Loaded += async (_, _) => await LoadVersionsAsync();
        panel.Children.Add(new TextBlock { Text = existing is null ? "NEW INSTANCE" : "INSTANCE SETTINGS", Foreground = (System.Windows.Media.Brush)Application.Current.Resources["Accent"], FontFamily = new System.Windows.Media.FontFamily("Consolas"), FontSize = 10, Margin = new Thickness(0, 2, 0, 8) });
        panel.Children.Add(new TextBlock { Text = existing is null ? "Set up a profile" : "Edit your setup", FontSize = 23, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 0, 0, 18) });
        AddField(panel, "INSTANCE NAME", _nameBox);
        _nameBox.Text = existing?.Name ?? "My Minecraft instance";
        AddField(panel, "MINECRAFT VERSION", _versionBox);
        _versionBox.IsEditable = true;
        _versionBox.IsTextSearchEnabled = true;
        _versionBox.StaysOpenOnEdit = true;
        _versionBox.Text = existing?.MinecraftVersion ?? "1.21.4";
        _versionStatus.Text = "Loading official releases and snapshots…";
        _versionStatus.Foreground = (System.Windows.Media.Brush)Application.Current.Resources["MutedText"];
        _versionStatus.FontSize = 10;
        _versionStatus.Margin = new Thickness(0, -9, 0, 10);
        panel.Children.Add(_versionStatus);
        AddField(panel, "MEMORY LIMIT (GB)", _memoryBox);
        _memoryBox.Text = (existing?.MemoryLimitGb ?? 4).ToString();
        panel.Children.Add(new TextBlock { Text = "JAVA EXECUTABLE · OPTIONAL, LEAVE BLANK TO USE PATH", Foreground = (System.Windows.Media.Brush)Application.Current.Resources["MutedText"], FontFamily = new System.Windows.Media.FontFamily("Consolas"), FontSize = 9, Margin = new Thickness(0, 4, 0, 6) });
        _javaPathBox.Text = existing?.JavaExecutablePath ?? "";
        _javaPathBox.Margin = new Thickness(0, 0, 8, 13);
        var javaRow = new DockPanel();
        var browseJava = new Button { Content = "Browse", MinWidth = 70, Margin = new Thickness(0, 0, 0, 13) };
        browseJava.Click += BrowseJava_Click;
        DockPanel.SetDock(browseJava, Dock.Right);
        javaRow.Children.Add(browseJava);
        javaRow.Children.Add(_javaPathBox);
        panel.Children.Add(javaRow);
        panel.Children.Add(new TextBlock { Text = "LOADER", Foreground = (System.Windows.Media.Brush)Application.Current.Resources["MutedText"], FontFamily = new System.Windows.Media.FontFamily("Consolas"), FontSize = 9, Margin = new Thickness(0, 4, 0, 6) });
        _loaderBox.Items.Add("Chunky (experimental alpha)");
        _loaderBox.Items.Add("Vanilla");
        _loaderBox.SelectedIndex = existing?.Loader == "Vanilla" ? 1 : 0;
        _loaderBox.Foreground = (System.Windows.Media.Brush)Application.Current.Resources["WindowBg"];
        _loaderBox.Background = System.Windows.Media.Brushes.WhiteSmoke;
        _loaderBox.Padding = new Thickness(9, 7, 9, 7);
        panel.Children.Add(_loaderBox);
        panel.Children.Add(new TextBlock { Text = "The Java path is saved for this instance. Leave it blank to use Java found on PATH. Install game files, a compatible Java runtime, and a Minecraft account before launching Vanilla.", Foreground = (System.Windows.Media.Brush)Application.Current.Resources["MutedText"], FontSize = 11, TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 13, 0, 8) });
        var actions = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right, Margin = new Thickness(0, 5, 0, 0) };
        var cancel = new Button { Content = "Cancel", Margin = new Thickness(0, 0, 8, 0), MinWidth = 80 };
        cancel.Click += (_, _) => { DialogResult = false; Close(); };
        var create = new Button { Content = existing is null ? "Create profile" : "Save settings", MinWidth = 110, Background = (System.Windows.Media.Brush)Application.Current.Resources["Accent"], Foreground = (System.Windows.Media.Brush)Application.Current.Resources["WindowBg"] };
        create.Click += Create_Click;
        actions.Children.Add(cancel);
        actions.Children.Add(create);
        panel.Children.Add(actions);
        _nameBox.Focus();
        _nameBox.SelectAll();
    }

    private static void AddField(Panel panel, string label, TextBox input)
    {
        panel.Children.Add(new TextBlock { Text = label, Foreground = (System.Windows.Media.Brush)Application.Current.Resources["MutedText"], FontFamily = new System.Windows.Media.FontFamily("Consolas"), FontSize = 9, Margin = new Thickness(0, 4, 0, 6) });
        input.Margin = new Thickness(0, 0, 0, 13);
        panel.Children.Add(input);
    }

    private static void AddField(Panel panel, string label, ComboBox input)
    {
        panel.Children.Add(new TextBlock { Text = label, Foreground = (System.Windows.Media.Brush)Application.Current.Resources["MutedText"], FontFamily = new System.Windows.Media.FontFamily("Consolas"), FontSize = 9, Margin = new Thickness(0, 4, 0, 6) });
        input.Margin = new Thickness(0, 0, 0, 13);
        panel.Children.Add(input);
    }

    private async Task LoadVersionsAsync()
    {
        try
        {
            var versions = await new MinecraftVersionCatalog().GetRecentVersionsAsync();
            var selected = _versionBox.Text;
            _versionBox.ItemsSource = versions;
            _versionBox.Text = selected;
            _versionStatus.Text = $"Official list · {versions.Count} recent releases and snapshots. You can still enter another version.";
        }
        catch (Exception exception) when (exception is HttpRequestException or TaskCanceledException or InvalidDataException or System.Text.Json.JsonException)
        {
            _versionStatus.Text = "Couldn't load the official list. Enter a version manually while offline.";
        }
    }

    private void BrowseJava_Click(object sender, RoutedEventArgs e)
    {
        var picker = new Microsoft.Win32.OpenFileDialog
        {
            Title = "Choose a Java runtime",
            Filter = "Java executable (javaw.exe; java.exe)|javaw.exe;java.exe|All files (*.*)|*.*",
            CheckFileExists = true,
        };
        if (picker.ShowDialog(this) == true) _javaPathBox.Text = picker.FileName;
    }

    private void Create_Click(object sender, RoutedEventArgs e)
    {
        var name = _nameBox.Text.Trim();
        var version = _versionBox.Text.Trim();
        if (name.Length is < 1 or > 48)
        {
            MessageBox.Show(this, "Use an instance name from 1 to 48 characters.", "Check the name", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }
        if (!Regex.IsMatch(version, @"^[A-Za-z0-9][A-Za-z0-9._+-]{0,31}$") || version.Contains("..", StringComparison.Ordinal))
        {
            MessageBox.Show(this, "Enter a version label using letters, numbers, dots, dashes, or underscores.", "Check the version", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }
        if (!int.TryParse(_memoryBox.Text.Trim(), out var memoryGb) || memoryGb is < 1 or > 32)
        {
            MessageBox.Show(this, "Choose a memory limit from 1 to 32 GB.", "Check the memory limit", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }
        var javaPath = _javaPathBox.Text.Trim();
        if (javaPath.Length > 0 && (!File.Exists(javaPath) || !Path.GetFileName(javaPath).Equals("java.exe", StringComparison.OrdinalIgnoreCase) && !Path.GetFileName(javaPath).Equals("javaw.exe", StringComparison.OrdinalIgnoreCase)))
        {
            MessageBox.Show(this, "Choose an existing java.exe or javaw.exe file, or leave this field blank to use Java on PATH.", "Check Java runtime", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }
        var loader = _loaderBox.SelectedIndex == 0 ? "Chunky" : "Vanilla";
        Result = new InstanceProfile
        {
            Id = _original?.Id ?? Guid.NewGuid(),
            Name = name,
            MinecraftVersion = version,
            Loader = loader,
            JavaExecutablePath = javaPath,
            RequiredJavaMajorVersion = _original?.RequiredJavaMajorVersion ?? 0,
            InstalledMinecraftVersion = _original?.InstalledMinecraftVersion ?? "",
            InstalledUtc = _original?.InstalledUtc,
            InstanceDirectory = _original?.InstanceDirectory ?? "",
            MemoryLimitMb = memoryGb * 1024,
            CreatedUtc = _original?.CreatedUtc ?? DateTime.UtcNow,
        };
        DialogResult = true;
        Close();
    }
}
