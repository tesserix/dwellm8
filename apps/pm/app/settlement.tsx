import React, { useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import {
  ActionBar, BackHeader, Button, Card, ChoiceRow, Field, KeyValue, Screen, StatusPill, Toast,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import type { ConnectMerchant, MerchantAccount, Tone } from '@dwellm8/mobile-shared';
import { connectMerchant, refreshMerchant, useMerchants } from '../src/data/merchant';

/**
 * The manager's own settlement account (#269). Rent lands in their account with
 * their aggregator, never in a Dwellm8 one, so this screen is the whole of
 * their money setup — and until it says verified, nothing may be collected.
 */

const stateTone: Record<string, Tone> = {
  verified: 'green', submitted: 'amber', suspended: 'red', unconnected: 'neutral',
};

const BUSINESS_TYPES: { code: ConnectMerchant['business_type']; label: string; hint: string }[] = [
  { code: 'proprietorship', label: 'Sole proprietorship', hint: 'You trade in your own name or a trade name' },
  { code: 'individual', label: 'Individual', hint: 'You manage in your personal capacity' },
  { code: 'partnership', label: 'Partnership', hint: 'A registered partnership firm' },
  { code: 'llp', label: 'LLP', hint: 'A limited liability partnership' },
  { code: 'company', label: 'Company', hint: 'Private or public limited' },
  { code: 'trust', label: 'Trust or society', hint: 'Including a registered association' },
];

export default function Settlement() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { loading, error, data: accounts } = useMerchants();

  const [businessName, setBusinessName] = useState('');
  const [businessType, setBusinessType] = useState<ConnectMerchant['business_type']>('proprietorship');
  const [pan, setPan] = useState('');
  const [gstin, setGstin] = useState('');
  const [holder, setHolder] = useState('');
  const [accountNumber, setAccountNumber] = useState('');
  const [ifsc, setIfsc] = useState('');
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  const ready = businessName.trim() && pan.trim() && holder.trim() && accountNumber.trim() && ifsc.trim();

  const say = (text: string) => {
    setToast(text);
    setTimeout(() => setToast(null), 3500);
  };

  const submit = async () => {
    setBusy(true);
    setFailure(null);
    try {
      const out = await connectMerchant({
        provider: 'cashfree',
        business_name: businessName.trim(),
        business_type: businessType,
        pan: pan.trim().toUpperCase(),
        gstin: gstin.trim().toUpperCase() || undefined,
        account_number: accountNumber.trim(),
        account_holder: holder.trim(),
        ifsc: ifsc.trim().toUpperCase(),
      });
      // The number is gone from this device the moment the call returns.
      setAccountNumber('');
      say(out.next_action);
    } catch (err) {
      setFailure((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const recheck = async (a: MerchantAccount) => {
    setBusy(true);
    try {
      say((await refreshMerchant(a.provider)).next_action);
    } catch (err) {
      setFailure((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <BackHeader title="Where rent settles" subtitle="Your own account, your own aggregator" onBack={goBack} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        {loading ? <View style={s.wait}><ActivityIndicator /></View> : null}
        {error ? <Text style={s.empty}>{error}</Text> : null}

        {accounts.map((a) => (
          <Card key={a.provider}>
            <View style={s.head}>
              <Text style={s.q}>{a.business_name || a.provider}</Text>
              <StatusPill text={a.state} tone={stateTone[a.state] ?? 'neutral'} />
            </View>
            <Text style={s.sub}>{a.next_action}</Text>
            <KeyValue k="Bank account" v={a.settlement_masked} />
            {a.settlement_ifsc ? <KeyValue k="IFSC" v={a.settlement_ifsc} /> : null}
            <KeyValue k="Settles in" v={a.settlement_currency} last />
            {a.state !== 'verified' ? (
              <Button label="Check again" tone="ghost" onPress={() => recheck(a)} disabled={busy} />
            ) : null}
          </Card>
        ))}

        {!loading && !accounts.length ? (
          <Card>
            <Text style={s.q}>Connect your account</Text>
            <Text style={s.sub}>
              Rent is collected into your own account, not ours. Your aggregator verifies the bank
              account first — we send them these details once and keep only the last four digits.
            </Text>
            <Field label="Business name" value={businessName} onChange={setBusinessName} placeholder="Menon Properties" autoCapitalize="words" />
            {BUSINESS_TYPES.map((b, i) => (
              <ChoiceRow
                key={b.code}
                label={b.label}
                hint={b.hint}
                selected={businessType === b.code}
                onPress={() => setBusinessType(b.code)}
                last={i === BUSINESS_TYPES.length - 1}
              />
            ))}
            <Field label="PAN" value={pan} onChange={setPan} placeholder="ABCDE1234F" autoCapitalize="characters" />
            <Field label="GSTIN — optional" value={gstin} onChange={setGstin} placeholder="29ABCDE1234F1Z5" autoCapitalize="characters" />
            <Field label="Account holder, as the bank has it" value={holder} onChange={setHolder} placeholder="Menon Properties" autoCapitalize="words" />
            <Field label="Bank account number" value={accountNumber} onChange={setAccountNumber} placeholder="50100123454321" keyboardType="numeric" />
            <Field label="IFSC" value={ifsc} onChange={setIfsc} placeholder="HDFC0001234" autoCapitalize="characters" />
            {failure ? <Text style={s.failure}>{failure}</Text> : null}
          </Card>
        ) : null}

        {failure && accounts.length ? <Text style={s.failure}>{failure}</Text> : null}
      </Screen>

      {!loading && !accounts.length ? (
        <ActionBar>
          <Button label={busy ? 'Sending…' : 'Connect account'} onPress={submit} disabled={!ready || busy} />
        </ActionBar>
      ) : null}
    </>
  );
}

const s = StyleSheet.create({
  head: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginBottom: space(2) },
  q: { ...font.h3, color: color.ink, flexShrink: 1 },
  sub: { ...font.body, color: color.inkSoft, marginBottom: space(3) },
  wait: { paddingVertical: space(6), alignItems: 'center' },
  empty: { ...font.body, color: color.inkSoft, paddingVertical: space(6), textAlign: 'center' },
  failure: { ...font.body, color: color.negative, marginTop: space(2) },
});
