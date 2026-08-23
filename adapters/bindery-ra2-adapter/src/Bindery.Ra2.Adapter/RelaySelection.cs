// SPDX-License-Identifier: GPL-3.0-or-later
namespace Bindery.Ra2.Adapter;

public static class RelaySelection
{
    public static string ProviderName(RelayProvider provider) => provider switch
    {
        RelayProvider.BinderyNative => "bindery-native",
        RelayProvider.CncNetBaseline => "cncnet-baseline",
        _ => throw new ArgumentOutOfRangeException(nameof(provider)),
    };

    public static void Validate(AdapterConfiguration configuration)
    {
        if (configuration.RelayProvider == RelayProvider.BinderyNative && string.IsNullOrWhiteSpace(configuration.RelayCredential)) throw new InvalidOperationException("native relay requires a scoped transport credential");
        if (configuration.RelayProvider == RelayProvider.CncNetBaseline && !string.IsNullOrWhiteSpace(configuration.RelayCredential)) throw new InvalidOperationException("CnCNet baseline must not receive a Bindery transport credential");
    }
}

