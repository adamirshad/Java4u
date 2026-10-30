# Microsoft sign-in setup

Java4u uses Microsoft's device-code flow and never asks for a Microsoft password. A public release needs its own Microsoft application registration; do not use another launcher's client ID.

## Create the Java4u registration

1. In the Microsoft Entra admin center, create an app registration named `Java4u`.
2. Choose account support that includes personal Microsoft accounts, since Minecraft ownership is checked on a player's Microsoft account.
3. Under **Authentication → Advanced settings**, enable **Allow public client flows**. Device-code flow requires a public-client registration.
4. Do not create or ship a client secret. A desktop launcher is a public client and cannot keep a shared secret confidential.
5. Copy the **Application (client) ID** into Java4u under **Settings**.
6. Confirm the registration and delegated permission setup meet the current Microsoft and Minecraft service requirements before distributing builds.

Microsoft's official [device-code flow guide](https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-device-code) and [desktop app registration guide](https://learn.microsoft.com/en-us/entra/identity-platform/scenario-desktop-app-registration) describe the public-client configuration.

## Minecraft service approval

Microsoft identity registration alone does not guarantee that Minecraft's Xbox and Minecraft services will accept a new launcher. Java4u must be approved by the relevant Minecraft service registration process before public users can complete the Minecraft entitlement and profile checks. Java4u reports an actionable error if Minecraft returns `Invalid app registration`.

That approval is controlled by Microsoft/Mojang and cannot be granted by the launcher code. Until it is approved, use a private development build only with an app registration and test account that the services accept. Never ask players to paste passwords, access tokens, or refresh tokens into an issue or support message.
