import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import {
  ActivityRow, AppHeader, AvatarButton, Card, ClipboardIcon, DottedRule,
  MoneyRow, Screen, SectionTitle, Segmented, color, font, inr, radius, space,
} from '@dwellm8/mobile-shared';
import { currentInvoice, methods } from '../../src/data/mock';
import { pay, useLiveData, type LiveData } from '../../src/data/source';

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
  const chosen = methods.find((m) => m.id === method)!;
  const totalDue = data.dueMinor;
  const payable = totalDue + chosen.feePaise;

  // In live mode the payment goes to the ledger through the provider chain
  // (ADR-0011); the UPI URL comes back and the OS opens it. Demo mode keeps
  // the walkthrough screens.
  const start = async () => {
    if (data.mode === 'demo' || !data.leaseId) {
      router.push(method === 'autopay' ? '/autopay' : '/pay-confirm');
      return;
    }
    if (method === 'autopay') {
      router.push('/autopay');
      return;
    }
    setBusy(true);
    try {
      const out = await pay(data.leaseId, totalDue, method === 'offline' ? 'offline' : 'upi');
      router.push(`/pay-confirm?status=${out.status}${out.payUrl ? `&url=${encodeURIComponent(out.payUrl)}` : ''}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <SectionTitle style={{ marginTop: space(4) }}>What you owe</SectionTitle>
      <Card>
        {data.mode === 'demo' ? (
          <>
            <Text style={s.invNo}>Invoice {currentInvoice.number}</Text>
            <Text style={s.invPeriod}>{currentInvoice.period}</Text>
            <View style={{ height: space(3) }} />
            {currentInvoice.lines.map((l) => (
              <MoneyRow key={l.label} label={l.label} value={inr(l.paise)} />
            ))}
          </>
        ) : (
          <>
            <Text style={s.invNo}>{data.tenancy.unit}</Text>
            <Text style={s.invPeriod}>{data.dueAsOf ? `As of ${data.dueAsOf}` : 'Current position'}</Text>
            <View style={{ height: space(3) }} />
          </>
        )}
        <MoneyRow label="Total due" value={inr(totalDue)} strong last />
      </Card>

      <SectionTitle>How you'd like to pay</SectionTitle>
      {methods.map((m) => {
        const active = m.id === method;
        return (
          <Pressable key={m.id} onPress={() => setMethod(m.id)}>
            <Card style={[s.method, active && s.methodActive]}>
              <View style={s.radio}>{active ? <View style={s.radioDot} /> : null}</View>
              <View style={{ flex: 1 }}>
                <Text style={s.methodLabel}>{m.label}</Text>
                <Text style={s.methodHint}>{m.hint}</Text>
              </View>
              {m.feePaise > 0 ? (
                <Text style={s.methodFee}>+{inr(m.feePaise)}</Text>
              ) : (
                <Text style={s.methodFree}>No charge</Text>
              )}
            </Card>
          </Pressable>
        );
      })}

      {chosen.feePaise > 0 ? (
        <View style={s.disclosure}>
          <Text style={s.disclosureText}>
            {inr(chosen.feePaise)} is the card processing charge, passed through at cost.
            Paying by UPI avoids it entirely.
          </Text>
        </View>
      ) : null}

      <Card>
        <MoneyRow label="Amount due" value={inr(totalDue)} />
        {chosen.feePaise > 0 ? <MoneyRow label="Payment charge" value={inr(chosen.feePaise)} /> : null}
        <MoneyRow label="You pay" value={inr(payable)} strong last />
      </Card>

      <Pressable style={[s.cta, busy && { opacity: 0.6 }]} onPress={start} disabled={busy}>
        <Text style={s.ctaText}>
          {busy ? 'Starting…'
            : method === 'offline' ? 'Record this payment'
            : method === 'autopay' ? 'Set up autopay'
            : `Pay ${inr(payable)}`}
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
          title={`${r.period} — ${inr(r.paise)}`}
          subtitle={<Text style={s.recSub}>{r.n} · {r.method}</Text>}
          meta={r.date}
        />
      ))}
      <Card style={{ marginTop: space(3) }}>
        <Text style={s.hraTitle}>Rent paid statement</Text>
        <Text style={s.hraBody}>
          Download a consolidated statement of rent paid for the financial year — the document
          your employer asks for when claiming HRA.
        </Text>
        <Pressable style={s.secondary}>
          <Text style={s.secondaryText}>Download 2025–26 statement</Text>
        </Pressable>
      </Card>
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
  methodFee: { ...font.label, color: color.negative },
  methodFree: { ...font.small, color: color.positive },

  disclosure: {
    backgroundColor: '#FDF3E7', borderRadius: radius.md,
    marginHorizontal: space(4), marginBottom: space(3), padding: space(4),
  },
  disclosureText: { ...font.small, color: '#8A5A18', lineHeight: 19 },

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
  hraTitle: { ...font.h3, color: color.inkStrong },
  hraBody: { ...font.body, color: color.ink, marginTop: 6, lineHeight: 22 },
  secondary: {
    borderWidth: 1.6, borderColor: color.accent, borderRadius: radius.pill,
    paddingVertical: space(3), alignItems: 'center', marginTop: space(4),
  },
  secondaryText: { ...font.label, color: color.accent },
});
