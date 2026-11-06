using System.IO;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;

namespace Java4u;

public sealed record MinecraftAuthSession(
    string PlayerName,
    string PlayerUuid,
    string Xuid,
    string MinecraftAccessToken,
    string MicrosoftRefreshToken,
    DateTimeOffset ExpiresUtc);

public sealed class MinecraftAuthStore
{
    private const int UiForbidden = 0x1;
    private readonly string _path = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Java4u", "account.bin");

    public async Task<MinecraftAuthSession?> LoadAsync(CancellationToken cancellationToken = default)
    {
        if (!File.Exists(_path)) return null;
        var protectedBytes = await File.ReadAllBytesAsync(_path, cancellationToken);
        var plainBytes = Unprotect(protectedBytes);
        try { return JsonSerializer.Deserialize<MinecraftAuthSession>(plainBytes); }
        finally { CryptographicOperations.ZeroMemory(plainBytes); }
    }

    public async Task SaveAsync(MinecraftAuthSession session, CancellationToken cancellationToken = default)
    {
        var plainBytes = JsonSerializer.SerializeToUtf8Bytes(session);
        var protectedBytes = Protect(plainBytes);
        CryptographicOperations.ZeroMemory(plainBytes);
        var folder = Path.GetDirectoryName(_path)!;
        Directory.CreateDirectory(folder);
        var tempPath = _path + ".tmp";
        try
        {
            await File.WriteAllBytesAsync(tempPath, protectedBytes, cancellationToken);
            File.Move(tempPath, _path, overwrite: true);
        }
        finally
        {
            CryptographicOperations.ZeroMemory(protectedBytes);
            if (File.Exists(tempPath)) File.Delete(tempPath);
        }
    }

    public void Delete()
    {
        if (File.Exists(_path)) File.Delete(_path);
    }

    private static byte[] Protect(byte[] data)
    {
        var input = ToBlob(data);
        try
        {
            if (!CryptProtectData(ref input, "Java4u Minecraft account", IntPtr.Zero, IntPtr.Zero, IntPtr.Zero, UiForbidden, out var output))
                throw new CryptographicException(Marshal.GetLastWin32Error());
            return CopyAndFree(output);
        }
        finally
        {
            Marshal.Copy(new byte[input.Size], 0, input.Data, input.Size);
            Marshal.FreeHGlobal(input.Data);
        }
    }

    private static byte[] Unprotect(byte[] data)
    {
        var input = ToBlob(data);
        try
        {
            if (!CryptUnprotectData(ref input, IntPtr.Zero, IntPtr.Zero, IntPtr.Zero, IntPtr.Zero, UiForbidden, out var output))
                throw new CryptographicException(Marshal.GetLastWin32Error());
            return CopyAndFree(output);
        }
        finally
        {
            Marshal.Copy(new byte[input.Size], 0, input.Data, input.Size);
            Marshal.FreeHGlobal(input.Data);
        }
    }

    private static DataBlob ToBlob(byte[] bytes)
    {
        var pointer = Marshal.AllocHGlobal(bytes.Length);
        Marshal.Copy(bytes, 0, pointer, bytes.Length);
        return new DataBlob { Size = bytes.Length, Data = pointer };
    }

    private static byte[] CopyAndFree(DataBlob blob)
    {
        try
        {
            var bytes = new byte[blob.Size];
            Marshal.Copy(blob.Data, bytes, 0, bytes.Length);
            return bytes;
        }
        finally
        {
            if (blob.Size > 0) Marshal.Copy(new byte[blob.Size], 0, blob.Data, blob.Size);
            LocalFree(blob.Data);
        }
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct DataBlob
    {
        public int Size;
        public IntPtr Data;
    }

    [DllImport("crypt32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool CryptProtectData(ref DataBlob dataIn, string description, IntPtr optionalEntropy, IntPtr reserved, IntPtr prompt, int flags, out DataBlob dataOut);

    [DllImport("crypt32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool CryptUnprotectData(ref DataBlob dataIn, IntPtr description, IntPtr optionalEntropy, IntPtr reserved, IntPtr prompt, int flags, out DataBlob dataOut);

    [DllImport("kernel32.dll")]
    private static extern IntPtr LocalFree(IntPtr memory);
}
