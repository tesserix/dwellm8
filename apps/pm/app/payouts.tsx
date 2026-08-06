import React, { useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Button, Card, EmptyState, Field, KeyValue, ListRow, Metric, Screen, StatusPill, Toast,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Settlement, Tone } from '@dwellm8/mobile-shared';
import { releaseSettlement, useSettlements } from '../src/data/settlements';

/**
 * Owner payouts (#270). Every share on this screen was computed once, when the
 * payment was captured, and is read back here — the platform's fee was retained
 * by the aggregator, so nothing is charged again at release.
 */

const stateTone: Record<Settlement['state'], Tone> = {
  pending: 'blue', instructed: 'amber', settled: 'green', failed: 'red',
};

const stateLabel: Record<Settlement['state'], string> = {
  pending: 'Ready', instructed: 'Sent', settled: 'Settled', failed: 'Blocked',
};

export default function Payouts() {
  const router = useRouter();
  const { loading, error, data: settlements } = useSettlements();
  const [open, setOpen] = useState<string | null>(null);
  const [beneficiary, setBeneficiary] = useState('');
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  const ready = settlements.filter((s) => s.state === 'pending');
  const late = settlements.filter((s) => s.overdue);
  const owed = ready.reduce((a, s) => a + s.owner_amount_minor, 0);

  const release = async (s: Settlement) => {
    setBusy(true);
    setFailure(null);
    try {
      await releaseSettlement(s.id, beneficiary.trim());
      setBeneficiary('');
      setToast('Sent to your provider — the UTR appears once they settle it');
      setTimeout(() => setToast(null), 3500);
    } catch (err) {
      setFailure((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <BackHeader title="Owner payouts" subtitle="What you are holding for somebody else" onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        {loading ? <View style={s.wait}><ActivityIndicator /></View> : null}
        {error ? <Text style={s.empty}>{error}</Text> : null}

        {settlements.length ? (
          <View style={s.metrics}>
            <Metric value={String(ready.length)} label="ready to release" tone="green" />
            <Metric value={inr(owed, { noPaise: true })} label="net to owners" tone="blue" />
            <Metric value={String(late.length)} label="past their date" tone={late.length ? 'red' : 'neutral'} />
          </View>
        ) : null}

        {settlements.map((row) => {
          const isOpen = open === row.id;
          return (
            <Card key={row.id} padded={false} style={{ paddingHorizontal: space(4), paddingVertical: space(2) }}>
              <ListRow
                title={inr(row.owner_amount_minor)}
                subtitle={row.lease_id ? `Tenancy ${row.lease_id.slice(0, 8)}` : row.payment_id.slice(0, 8)}
                meta={row.expected_on ? `Expected ${row.expected_on}` : 'No promised date'}
                right={<StatusPill text={row.overdue && row.state !== 'settled' ? 'Late' : stateLabel[row.state]} tone={row.overdue && row.state !== 'settled' ? 'red' : stateTone[row.state]} />}
                onPress={() => setOpen(isOpen ? null : row.id)}
                last
              />
              {isOpen ? (
                <View style={{ paddingBottom: space(3) }}>
                  <KeyValue k="Rent collected" v={inr(row.gross_amount_minor)} tone="green" />
                  <KeyValue k="Platform fee" v={`− ${inr(row.platform_amount_minor)}`} tone="red" />
                  <KeyValue k="Your management fee" v={`− ${inr(row.management_amount_minor)}`} tone="red" />
                  <KeyValue k="TDS withheld" v={`− ${inr(row.tds_amount_minor)}`} tone="red" />
                  <KeyValue k="Net to owner" v={inr(row.owner_amount_minor)} last={row.state === 'settled'} />
                  {row.transfer_ref ? <KeyValue k="Reference" v={row.transfer_ref} last /> : null}
                  {row.reason ? <Text style={s.failure}>{row.reason}</Text> : null}
                  {row.state === 'pending' || row.state === 'failed' ? (
                    <>
                      {/* The owner's beneficiary at the provider; registering it from here is #271. */}
                      <Field
                        label="Owner's beneficiary reference"
                        value={beneficiary}
                        onChange={setBeneficiary}
                        placeholder="BENE-0001"
                        autoCapitalize="characters"
                      />
                      <Button
                        label={busy ? 'Sending…' : 'Release payout'}
                        onPress={() => release(row)}
                        disabled={busy || !beneficiary.trim()}
                        style={{ marginTop: space(2) }}
                      />
                    </>
                  ) : null}
                </View>
              ) : null}
            </Card>
          );
        })}

        {!loading && !error && !settlements.length ? (
          <EmptyState
            title="Nothing owed onward"
            body="When rent is collected it is divided here — the platform's fee, your management fee, any TDS, and the owner's share."
          />
        ) : null}

        {failure ? <Text style={s.failure}>{failure}</Text> : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  wait: { paddingVertical: space(6), alignItems: 'center' },
  empty: { ...font.body, color: color.inkSoft, paddingVertical: space(6), textAlign: 'center' },
  failure: { ...font.body, color: color.negative, marginTop: space(2) },
});
