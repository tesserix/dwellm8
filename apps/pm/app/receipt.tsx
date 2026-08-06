import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Field, ChoiceRow, Button, ActionBar, KeyValue,
  Toast, StatusPill, SwitchRow,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { arrears, receiptMethods } from '../src/data/mock';

/**
 * Record a payment taken in the field.
 *
 * Cash collection is the reality of Indian property management, and it is
 * where money goes missing. The receipt is issued to the tenant the moment
 * this is saved, so the tenant's copy and the ledger cannot diverge.
 */

export default function RecordReceipt() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const target = arrears.find((a) => a.id === id) ?? arrears[0];

  const [amount, setAmount] = useState(String(Math.round(target.duePaise / 100)));
  const [method, setMethod] = useState(receiptMethods[0]);
  const [ref, setRef] = useState('');
  const [notify, setNotify] = useState(true);
  const [saved, setSaved] = useState(false);

  const paise = Math.round((Number(amount.replace(/[^0-9.]/g, '')) || 0) * 100);
  const remaining = Math.max(0, target.duePaise - paise);

  if (saved) {
    return (
      <>
        <BackHeader title="Receipt issued" onBack={() => router.back()} />
        <Screen>
          <Toast text="Posted to the ledger and sent to the tenant" />
          <Card>
            <StatusPill text="RCT/2026-27/0433" tone="green" />
            <Text style={s.amount}>{inr(paise)}</Text>
            <KeyValue k="From" v={target.tenant} />
            <KeyValue k="Unit" v={target.unit} />
            <KeyValue k="Method" v={method} />
            <KeyValue k="Collected by" v="Ritika Nambiar" />
            <KeyValue k="Balance remaining" v={inr(remaining)} tone={remaining ? 'amber' : 'green'} last />
            <Text style={s.note}>
              The tenant has the receipt on WhatsApp and in their app. The owner sees it on the next
              statement; the platform fee is charged once, at payout.
            </Text>
          </Card>
          <Button label="Done" onPress={() => router.back()} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Record a payment" subtitle={target.unit} onBack={() => router.back()} />
      <Screen>
        <Card>
          <Text style={s.h}>{target.tenant}</Text>
          <Text style={s.sub}>{inr(target.duePaise)} outstanding since {target.dueSince}</Text>
          <View style={{ marginTop: space(4) }}>
            <Field label="Amount received (₹)" value={amount} onChange={setAmount} keyboardType="numeric" />
            {paise > 0 ? (
              <Text style={s.calc}>
                {inr(paise)} received · {remaining ? `${inr(remaining)} will remain outstanding` : 'settles the balance in full'}
              </Text>
            ) : null}
          </View>
        </Card>

        <Card>
          <Text style={s.h}>How was it paid?</Text>
          {receiptMethods.map((m, i) => (
            <ChoiceRow
              key={m}
              label={m}
              hint={m === 'Cash' ? 'You are accountable for the cash until it is banked' : undefined}
              selected={method === m}
              onPress={() => setMethod(m)}
              last={i === receiptMethods.length - 1}
            />
          ))}
        </Card>

        <Card>
          <Field label="Reference (UTR, cheque number)" value={ref} onChange={setRef} placeholder="Optional" />
          <SwitchRow
            label="Send the receipt to the tenant"
            hint="WhatsApp and in-app, immediately on save"
            value={notify}
            onChange={setNotify}
            last
          />
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Cancel" tone="secondary" onPress={() => router.back()} style={{ flex: 1 }} />
        <Button label="Issue receipt" onPress={() => setSaved(true)} disabled={paise <= 0} style={{ flex: 2 }} />
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
});
