import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import {
  ActivityRow, AppHeader, AvatarButton, Card, ClipboardIcon,
  MoneyRow, Screen, SectionTitle, Segmented, color, font, inr, radius, space,
} from '@dwellm8/mobile-shared';
import { pay, payMethods, useLiveData, type LiveData } from '../../src/data/source';

export default function Pay() {
  const router = useRouter();
  const [tab, setTab] = useState('Pay');
  const data = useLiveData();

  return (
    <>
      <AppHeader title="Payments" showCaret={false} left={<AvatarButton onPress={() => router.push('/profile')} />} />
      <View style={{ backgroundColor: '#FFF', paddingBottom: space(3) }}>
        <Segmented items={['Pay', 'Receipts']} value={tab} onChange={setTab} />
      </View>
      <Screen>
        {tab === 'Pay' ? <PayTab data={data} /> : <ReceiptsTab data={data} />}
      </Screen>
    </>
  );
}

function PayTab({ data }: { data: LiveData }) {
  const router = useRouter();
  const [method, setMethod] = useState('upi');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const totalDue = data.dueMinor;

  // The payment goes to the ledger through the provider chain (ADR-0011); the
  // UPI URL comes back and pay-confirm opens it.
  const start = async () => {
    if (!data.leaseId || busy || totalDue <= 0) return;
    setBusy(true);
    setError(null);
    try {
      const out = await pay(data.leaseId, totalDue, method === 'offline' ? 'offline' : 'upi');
      router.push(`/pay-confirm?status=${out.status}&amount=${totalDue}${out.payUrl ? `&url=${encodeURIComponent(out.payUrl)}` : ''}`);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <SectionTitle style={{ marginTop: space(4) }}>What you owe</SectionTitle>
      <Card>
        <Text style={s.invNo}>{data.tenancy.unit}</Text>
        <Text style={s.invPeriod}>{data.dueAsOf ? `As of ${data.dueAsOf}` : 'Current position'}</Text>
        <View style={{ height: space(3) }} />
        <MoneyRow label="Total due" value={inr(totalDue)} strong last />
      </Card>

      <SectionTitle>How you'd like to pay</SectionTitle>
      {payMethods.map((m) => {
        const active = m.id === method;
        return (
          <Pressable key={m.id} onPress={() => setMethod(m.id)}>
            <Card style={[s.method, active && s.methodActive]}>
              <View style={s.radio}>{active ? <View style={s.radioDot} /> : null}</View>
              <View style={{ flex: 1 }}>
                <Text style={s.methodLabel}>{m.label}</Text>
                <Text style={s.methodHint}>{m.hint}</Text>
              </View>
              <Text style={s.methodFree}>No charge</Text>
            </Card>
          </Pressable>
        );
      })}

      {error ? <Text style={s.error}>{error}</Text> : null}

      <Pressable
        style={[s.cta, (busy || totalDue <= 0) && { opacity: 0.6 }]}
        onPress={start}
        disabled={busy || totalDue <= 0}
      >
        <Text style={s.ctaText}>
          {busy ? 'Starting…'
            : totalDue <= 0 ? 'Nothing due'
            : method === 'offline' ? 'Record this payment'
            : `Pay ${inr(totalDue)}`}
        </Text>
      </Pressable>
      <Text style={s.ctaNote}>
        Dwellm8 never adds a fee to your rent. Your receipt arrives the moment payment confirms.
      </Text>
    </>
  );
}

function ReceiptsTab({ data }: { data: LiveData }) {
  const receipts = data.receipts;
  return (
    <>
      <SectionTitle style={{ marginTop: space(4) }}>Your receipts</SectionTitle>
      {!receipts.length ? (
        <Card><Text style={s.recSub}>No receipts yet — they appear the moment a payment confirms.</Text></Card>
      ) : null}
      {receipts.map((r) => (
        <ActivityRow
          key={r.id}
          icon={<ClipboardIcon size={22} c={color.inkFaint} />}
          title={`Rent paid — ${inr(r.paise)}`}
          subtitle={<Text style={s.recSub}>{r.n} · {r.method}</Text>}
          meta={r.date}
        />
      ))}
    </>
  );
}

const s = StyleSheet.create({
  invNo: { ...font.label, color: color.inkSoft },
  invPeriod: { ...font.h3, color: color.inkStrong, marginTop: 3 },

  method: { flexDirection: 'row', alignItems: 'center', gap: 14, borderWidth: 2, borderColor: 'transparent' },
  methodActive: { borderColor: color.accent },
  radio: {
    width: 22, height: 22, borderRadius: 11, borderWidth: 2, borderColor: color.accent,
    alignItems: 'center', justifyContent: 'center',
  },
  radioDot: { width: 11, height: 11, borderRadius: 6, backgroundColor: color.accent },
  methodLabel: { ...font.title, color: color.inkStrong },
  methodHint: { ...font.small, color: color.inkSoft, marginTop: 3 },
  methodFree: { ...font.small, color: color.positive },

  error: {
    ...font.small, color: '#E0524E', textAlign: 'center',
    marginHorizontal: space(4), marginBottom: space(2),
  },

  cta: {
    backgroundColor: color.accent, borderRadius: radius.pill,
    marginHorizontal: space(4), paddingVertical: space(4), alignItems: 'center', marginTop: space(2),
  },
  ctaText: { ...font.h3, color: '#FFF' },
  ctaNote: {
    ...font.small, color: color.inkSoft, textAlign: 'center',
    marginHorizontal: space(6), marginTop: space(3), lineHeight: 19,
  },

  recSub: { ...font.small, color: color.inkSoft, marginTop: 3 },
});
