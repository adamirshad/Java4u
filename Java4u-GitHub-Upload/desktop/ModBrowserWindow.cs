using System.Collections.ObjectModel;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;

namespace Java4u;

public sealed class ModBrowserWindow : Window
{
    private readonly InstanceProfile _profile;
    private readonly ModrinthService _service = new();
    private readonly ObservableCollection<ModrinthProject> _projects = [];
    private readonly ObservableCollection<ModrinthVersion> _versions = [];
    private readonly TextBox _query = new();
    private readonly ListBox _projectList = new();
    private readonly ComboBox _versionList = new();
    private readonly TextBlock _details = new();
    private readonly TextBlock _status = new();
    private readonly Button _search = new() { Content = "Search" };
    private readonly Button _install = new() { Content = "Add selected mod", IsEnabled = false };
    private readonly Button _close = new() { Content = "Close", Padding = new Thickness(16, 8, 16, 8) };
    private CancellationTokenSource? _operationCancellation;
    private bool _busy;

    public ModBrowserWindow(InstanceProfile profile)
    {
        _profile = profile;
        Title = $"Java4u · Mods · {profile.Name}";
        Width = 820;
        Height = 650;
        MinWidth = 700;
        MinHeight = 560;
        WindowStartupLocation = WindowStartupLocation.CenterOwner;
        Background = Brushes.Transparent;
        AllowsTransparency = true;
        WindowStyle = WindowStyle.None;
        ShowInTaskbar = false;

        var root = new Border
        {
            Margin = new Thickness(18),
            Padding = new Thickness(24),
            CornerRadius = new CornerRadius(12),
            BorderThickness = new Thickness(1),
            BorderBrush = (Brush)Application.Current.Resources["PanelBorder"],
            Background = (Brush)Application.Current.Resources["PanelBg"],
        };
        Content = root;
        var layout = new Grid();
        layout.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        layout.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        layout.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        layout.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        layout.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        layout.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        layout.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        root.Child = layout;

        var heading = new StackPanel { Margin = new Thickness(0, 0, 0, 16) };
        heading.Children.Add(new TextBlock { Text = "MOD BROWSER", FontFamily = new FontFamily("Consolas"), FontSize = 10, Foreground = (Brush)Application.Current.Resources["Accent"] });
        heading.Children.Add(new TextBlock { Text = "Find Fabric mods", FontSize = 25, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 5, 0, 4) });
        heading.Children.Add(new TextBlock { Text = $"{profile.MinecraftVersion} · {profile.Loader} · results from Modrinth", Foreground = (Brush)Application.Current.Resources["MutedText"], FontSize = 12 });
        Grid.SetRow(heading, 0);
        layout.Children.Add(heading);

        var searchRow = new DockPanel { Margin = new Thickness(0, 0, 0, 12) };
        _search.Margin = new Thickness(8, 0, 0, 0);
        _search.Padding = new Thickness(15, 8, 15, 8);
        _query.Height = 36;
        _query.Padding = new Thickness(10, 7, 10, 7);
        _query.VerticalContentAlignment = VerticalAlignment.Center;
        _query.ToolTip = "Search by mod name or keyword";
        DockPanel.SetDock(_search, Dock.Right);
        searchRow.Children.Add(_search);
        searchRow.Children.Add(_query);
        Grid.SetRow(searchRow, 1);
        layout.Children.Add(searchRow);

        var resultsBorder = new Border { BorderBrush = (Brush)Application.Current.Resources["PanelBorder"], BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(6), Padding = new Thickness(4) };
        _projectList.ItemsSource = _projects;
        _projectList.DisplayMemberPath = nameof(ModrinthProject.Display);
        _projectList.BorderThickness = new Thickness(0);
        resultsBorder.Child = _projectList;
        Grid.SetRow(resultsBorder, 2);
        layout.Children.Add(resultsBorder);

        _details.Text = "Search Modrinth to see compatible Fabric projects.";
        _details.TextWrapping = TextWrapping.Wrap;
        _details.Margin = new Thickness(1, 12, 1, 12);
        _details.Foreground = (Brush)Application.Current.Resources["MutedText"];
        Grid.SetRow(_details, 3);
        layout.Children.Add(_details);

        var versionRow = new DockPanel { Margin = new Thickness(0, 0, 0, 10) };
        versionRow.Children.Add(new TextBlock { Text = "Version", VerticalAlignment = VerticalAlignment.Center, Margin = new Thickness(0, 0, 10, 0) });
        _versionList.Width = 390;
        _versionList.DisplayMemberPath = nameof(ModrinthVersion.Display);
        _versionList.ItemsSource = _versions;
        versionRow.Children.Add(_versionList);
        Grid.SetRow(versionRow, 4);
        layout.Children.Add(versionRow);

        _status.Text = "Mod files will be verified before they are added to this instance.";
        _status.TextWrapping = TextWrapping.Wrap;
        _status.Margin = new Thickness(0, 0, 0, 12);
        _status.Foreground = (Brush)Application.Current.Resources["MutedText"];
        Grid.SetRow(_status, 5);
        layout.Children.Add(_status);

        var actions = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right };
        _install.Padding = new Thickness(14, 8, 14, 8);
        _install.Background = (Brush)Application.Current.Resources["Accent"];
        _install.Foreground = (Brush)Application.Current.Resources["WindowBg"];
        _install.FontWeight = FontWeights.SemiBold;
        _install.Margin = new Thickness(0, 0, 8, 0);
        actions.Children.Add(_install);
        actions.Children.Add(_close);
        Grid.SetRow(actions, 6);
        layout.Children.Add(actions);

        _search.Click += async (_, _) => await SearchAsync();
        _query.KeyDown += async (_, e) => { if (e.Key == System.Windows.Input.Key.Enter) await SearchAsync(); };
        _projectList.SelectionChanged += async (_, _) => await LoadVersionsAsync();
        _versionList.SelectionChanged += (_, _) => _install.IsEnabled = CanInstallSelectedVersion();
        _install.Click += async (_, _) => await InstallAsync();
        _close.Click += (_, _) =>
        {
            if (_busy)
            {
                _status.Text = "Canceling the current request…";
                _operationCancellation?.Cancel();
            }
            else Close();
        };
        if (!profile.IsInstalled) _status.Text = "Install this instance's Minecraft files before adding mods.";
        else if (!string.Equals(profile.Loader, "Chunky", StringComparison.Ordinal)) _status.Text = "Mod files can only be added to a Chunky profile.";
        Loaded += (_, _) => _query.Focus();
    }

    private async Task SearchAsync()
    {
        if (_busy) return;
        await RunBusyAsync("Searching Modrinth…", async cancellationToken =>
        {
            _projects.Clear();
            _versions.Clear();
            _projectList.SelectedItem = null;
            _details.Text = "Choose a mod to inspect its compatible versions.";
            var results = await _service.SearchFabricModsAsync(_query.Text, _profile.MinecraftVersion, cancellationToken);
            foreach (var project in results) _projects.Add(project);
            _status.Text = results.Count == 0 ? "No results matched this Minecraft version. Try a different search." : $"Showing {results.Count} Fabric projects compatible with Minecraft {_profile.MinecraftVersion}.";
        });
    }

    private async Task LoadVersionsAsync()
    {
        if (_busy || _projectList.SelectedItem is not ModrinthProject project) return;
        _versions.Clear();
        _install.IsEnabled = false;
        _details.Text = $"{project.Title} by {project.Author}\n\n{project.Description}\n\n{project.Downloads:N0} downloads · {project.Slug}";
        await RunBusyAsync("Loading compatible mod versions…", async cancellationToken =>
        {
            var versions = await _service.GetFabricVersionsAsync(project.ProjectId, _profile.MinecraftVersion, cancellationToken);
            foreach (var version in versions) _versions.Add(version);
            _versionList.SelectedIndex = versions.Count > 0 ? 0 : -1;
            if (versions.Count == 0) _status.Text = "No Fabric version is available for this Minecraft version.";
        });
    }

    private async Task InstallAsync()
    {
        if (_busy || _versionList.SelectedItem is not ModrinthVersion version) return;
        await RunBusyAsync("Preparing mod download…", async cancellationToken =>
        {
            var progress = new Progress<string>(message => _status.Text = message);
            await _service.InstallAsync(_profile, version, progress, cancellationToken);
            _status.Text = "Mod files and required Modrinth dependencies are added with their checksums verified. Chunky must still be implemented before the game can run them.";
            MessageBox.Show(this, _status.Text, "Mods added", MessageBoxButton.OK, MessageBoxImage.Information);
        });
    }

    private async Task RunBusyAsync(string message, Func<CancellationToken, Task> action)
    {
        _busy = true;
        _operationCancellation = new CancellationTokenSource();
        _search.IsEnabled = false;
        _projectList.IsEnabled = false;
        _versionList.IsEnabled = false;
        _install.IsEnabled = false;
        _close.Content = "Cancel";
        _status.Text = message;
        try { await action(_operationCancellation.Token); }
        catch (OperationCanceledException)
        {
            _status.Text = "Request canceled. Any completed verified downloads are safe to reuse.";
        }
        catch (Exception exception)
        {
            _status.Text = "The Modrinth request didn't finish.";
            MessageBox.Show(this, exception.Message, "Mod browser", MessageBoxButton.OK, MessageBoxImage.Warning);
        }
        finally
        {
            _operationCancellation.Dispose();
            _operationCancellation = null;
            _busy = false;
            _search.IsEnabled = true;
            _projectList.IsEnabled = true;
            _versionList.IsEnabled = true;
            _install.IsEnabled = CanInstallSelectedVersion();
            _close.Content = "Close";
        }
    }

    private bool CanInstallSelectedVersion()
        => !_busy && _profile.IsInstalled && string.Equals(_profile.Loader, "Chunky", StringComparison.Ordinal) && _versionList.SelectedItem is not null;
}
