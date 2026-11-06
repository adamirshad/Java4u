using System.IO;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;

namespace Java4u;

public enum DownloadCenterAction { None, InstallGame, ModBrowser, OpenFolder }

public sealed class DownloadCenterWindow : Window
{
    public DownloadCenterAction Action { get; private set; }

    public DownloadCenterWindow(InstanceProfile profile, string lastStatus)
    {
        Title = "Java4u downloads";
        Width = 620;
        Height = 470;
        ResizeMode = ResizeMode.NoResize;
        WindowStartupLocation = WindowStartupLocation.CenterOwner;
        Background = Brushes.Transparent;
        AllowsTransparency = true;
        WindowStyle = WindowStyle.None;
        ShowInTaskbar = false;

        var panel = new StackPanel { Margin = new Thickness(27) };
        Content = new Border
        {
            Background = (Brush)Application.Current.Resources["PanelBg"],
            BorderBrush = (Brush)Application.Current.Resources["PanelBorder"],
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(10),
            Child = panel,
        };
        panel.Children.Add(new TextBlock { Text = "DOWNLOAD CENTER", FontFamily = new FontFamily("Consolas"), FontSize = 10, Foreground = (Brush)Application.Current.Resources["Accent"] });
        panel.Children.Add(new TextBlock { Text = profile.Name, FontSize = 25, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 6, 0, 3) });
        panel.Children.Add(new TextBlock { Text = $"Minecraft {profile.MinecraftVersion} · {profile.Loader}", FontSize = 12, Foreground = (Brush)Application.Current.Resources["MutedText"] });

        var minecraftState = profile.IsInstalled ? $"Installed · Java {profile.RequiredJavaMajorVersion}" : "Game files not installed";
        var modsPath = Path.Combine(profile.InstanceDirectory, ".minecraft", "mods");
        var modCount = Directory.Exists(modsPath) ? Directory.EnumerateFiles(modsPath, "*.jar").Count() : 0;
        panel.Children.Add(new Border
        {
            Margin = new Thickness(0, 19, 0, 16),
            Padding = new Thickness(14),
            BorderBrush = (Brush)Application.Current.Resources["PanelBorder"],
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(6),
            Child = new StackPanel
            {
                Children =
                {
                    new TextBlock { Text = "Minecraft files", FontWeight = FontWeights.SemiBold },
                    new TextBlock { Text = minecraftState, Margin = new Thickness(0, 4, 0, 12), Foreground = (Brush)Application.Current.Resources["MutedText"] },
                    new TextBlock { Text = "Mod files", FontWeight = FontWeights.SemiBold },
                    new TextBlock { Text = $"{modCount} jar file(s) in this instance", Margin = new Thickness(0, 4, 0, 0), Foreground = (Brush)Application.Current.Resources["MutedText"] },
                }
            }
        });

        panel.Children.Add(new TextBlock { Text = "Latest activity", FontSize = 11, FontWeight = FontWeights.SemiBold });
        var status = new TextBlock { Text = string.IsNullOrWhiteSpace(lastStatus) ? "No downloads have run in this session." : lastStatus, TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 4, 0, 15), Foreground = (Brush)Application.Current.Resources["MutedText"] };
        panel.Children.Add(status);

        var actions = new WrapPanel { HorizontalAlignment = HorizontalAlignment.Right };
        AddAction(actions, profile.IsInstalled ? "Repair game files" : "Install game files", DownloadCenterAction.InstallGame, primary: true);
        AddAction(actions, "Browse Fabric mods", DownloadCenterAction.ModBrowser);
        AddAction(actions, "Open instance folder", DownloadCenterAction.OpenFolder);
        AddAction(actions, "Close", DownloadCenterAction.None);
        panel.Children.Add(actions);
    }

    private void AddAction(Panel panel, string label, DownloadCenterAction action, bool primary = false)
    {
        var button = new Button { Content = label, Padding = new Thickness(11, 7, 11, 7), Margin = new Thickness(6, 0, 0, 0) };
        if (primary)
        {
            button.Background = (Brush)Application.Current.Resources["Accent"];
            button.Foreground = (Brush)Application.Current.Resources["WindowBg"];
            button.FontWeight = FontWeights.SemiBold;
        }
        button.Click += (_, _) =>
        {
            Action = action;
            DialogResult = true;
        };
        panel.Children.Add(button);
    }
}
