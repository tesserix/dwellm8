import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Field, ChoiceRow, Button, ActionBar, KeyValue,
  Toast, StatusPill,
  color, font, inr, space, useBack,
} from '@dwellm8/mobile-shared';
import { useRecordCollection, type CollectionMethod } from '../src/data/collection';

/**
 * Record a payment taken in the field.
 *
 * Cash collection is the reality of Indian property management, and it is
 * where money goes missing. The receipt posts to the ledger on save, so the
 * tenant's copy and the ledger cannot diverge.
 */

const methods: { id: CollectionMethod; label: string; hint?: string }[] = [
  { id: 'offline_cash', label: 'Cash', hint: 'You are accountable for the cash until it is banked' },
  { id: 'offline_cheque', label: 'Cheque' },
  { id: 'offline_transfer', label: 'Bank transfer (NEFT, IMPS, UPI to your account)' },
];

export default function RecordReceipt() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { lease, unit, tenant, due } = useLocalSearchParams<{
    lease?: string; unit?: string; tenant?: string; due?: string;
  }>();
  const owedPaise = Number(due ?? 0) || 0;

  const [amount, setAmount] = useState(owedPaise > 0 ? String(Math.round(owedPaise / 100)) : '');
  const [method, setMethod] = useState<CollectionMethod>('offline_cash');
  const [ref, setRef] = useState('');
  const { record, saving, error, result } = useRecordCollection(lease ?? '');

  const paise = Math.round((Number(amount.replace(/[^0-9.]/g, '')) || 0) * 100);

  if (!lease) {
    return (
      <>
        <BackHeader title="Record a payment" onBack={goBack} />
        <Screen>
          <Card><Text style={s.sub}>Open this from a tenancy, so the receipt has one to post against.</Text></Card>
        </Screen>
      </>
    );
  }

  if (result) {
    return (
      <>
        <BackHeader title="Receipt issued" onBack={goBack} />
        <Screen>
          <Toast text="Posted to the ledger" />
          <Card>
            <StatusPill text={result.status === 'captured' ? 'Captured' : result.status} tone={result.status === 'captured' ? 'green' : 'amber'} />
            <Text style={s.amount}>{inr(result.amount_minor)}</Text>
            <KeyValue k="From" v={tenant || 'the tenant'} />
            <KeyValue k="Unit" v={unit || '—'} />
            <KeyValue k="Method" v={methods.find((m) => m.id === result.method)?.label ?? result.method} />
            {ref ? <KeyValue k="Reference" v={ref} /> : null}
            <KeyValue
              k="Balance remaining"
              v={inr(result.due_amount_minor)}
              tone={result.due_amount_minor ? 'amber' : 'green'}
              last={!result.advance_amount_minor}
            />
            {result.advance_amount_minor ? (
              <KeyValue k="Held in advance" v={inr(result.advance_amount_minor)} tone="green" last />
            ) : null}
            <Text style={s.note}>
              The tenant sees this on their own statement. The owner sees it on the next one; the
              platform fee is charged once, at payout.
            </Text>
          </Card>
          <Button label="Done" onPress={goBack} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Record a payment" subtitle={unit} onBack={goBack} />
      <Screen>
        <Card>
          {error ? <Text style={s.error} accessibilityRole="alert">{error}</Text> : null}
          <Text style={s.h}>{tenant || 'This tenancy'}</Text>
          <Text style={s.sub}>
            {owedPaise > 0 ? `${inr(owedPaise)} outstanding` : 'Nothing outstanding — anything received is held in advance'}
          </Text>
          <View style={{ marginTop: space(4) }}>
            <Field label="Amount received (₹)" value={amount} onChange={setAmount} keyboardType="numeric" />
            {paise > 0 ? (
              <Text style={s.calc}>
                {inr(paise)} received{owedPaise > paise ? ` · ${inr(owedPaise - paise)} will remain outstanding` : ''}
              </Text>
            ) : null}
          </View>
        </Card>

        <Card>
          <Text style={s.h}>How was it paid?</Text>
          {methods.map((m, i) => (
            <ChoiceRow
              key={m.id}
              label={m.label}
              hint={m.hint}
              selected={method === m.id}
              onPress={() => setMethod(m.id)}
              last={i === methods.length - 1}
            />
          ))}
        </Card>

        <Card>
          <Field label="Reference (UTR, cheque number)" value={ref} onChange={setRef} placeholder="Optional" />
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Cancel" tone="secondary" onPress={goBack} style={{ flex: 1 }} />
        <Button
          label={saving ? 'Posting…' : 'Issue receipt'}
          onPress={() => record(paise, method, ref)}
          disabled={paise <= 0 || saving}
          style={{ flex: 2 }}
        />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong },
  sub: { ...font.body, color: color.inkSoft, marginTop: 4 },
  calc: { ...font.small, color: color.accent, fontWeight: '600' },
  amount: { ...font.h1, fontSize: 32, color: color.positive, marginVertical: space(3) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
  error: { ...font.small, color: color.negative, marginBottom: space(3) },
});
