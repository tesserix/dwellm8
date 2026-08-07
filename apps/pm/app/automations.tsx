import React, { useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Button, Card, Screen, KeyValue, StatusPill, SwitchRow, Toast, Metric,
  apiFromEnv, color, font, inr, space, useBack,
} from '@dwellm8/mobile-shared';
import { refreshWorklists, useOpsApprovals, useOpsAutomations } from '../src/data/worklists';

/** Automations (ADR-0033): the catalogue as it runs for this organisation,
 * and the approvals waiting on a human (GET /v1/automations). */

export default function Automations() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { loading, error, data: automations } = useOpsAutomations();
  const approvals = useOpsApprovals();
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const api = apiFromEnv();

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  const toggle = async (key: string, enabled: boolean) => {
    if (!api || busy) return;
    setBusy(true);
    try {
      await api.opsSetAutomation(key, { enabled });
      refreshWorklists();
    } catch (err) {
      say((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const decide = async (id: string, decision: 'approve' | 'decline') => {
    if (!api || busy) return;
    setBusy(true);
    try {
      await api.opsDecideApproval(id, decision, decision === 'decline' ? 'declined from the app' : undefined);
      refreshWorklists();
      say(decision === 'approve' ? 'Approved — the automation proceeds' : 'Declined');
    } catch (err) {
      say((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const enabled = automations.filter((a) => a.enabled).length;
  const awaiting = approvals.data.length;

  return (
    <>
      <BackHeader title="Automations" subtitle="What runs by itself, and its limits" onBack={goBack} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}
        <View style={s.metrics}>
          <Metric value={loading ? '…' : `${enabled}/${automations.length}`} label="running" tone="green" />
          <Metric value={approvals.loading ? '…' : String(awaiting)} label="awaiting approval" tone={awaiting ? 'amber' : 'green'} />
        </View>

        {awaiting > 0 ? (
          <>
            {approvals.data.map((a) => (
              <Card key={a.id}>
                <StatusPill text="Awaiting your approval" tone="amber" dot />
                <Text style={s.name}>{a.automation.replace(/_/g, ' ')}</Text>
                <KeyValue k="Wants to" v={a.action.replace(/_/g, ' ')} />
                <KeyValue k="Amount" v={inr(a.amount_minor)} />
                <KeyValue k="Your ceiling" v={inr(a.ceiling_minor)} last />
                <View style={{ flexDirection: 'row', gap: 10, marginTop: space(3) }}>
                  <Button label="Decline" tone="secondary" small onPress={() => decide(a.id, 'decline')} disabled={busy} style={{ flex: 1 }} />
                  <Button label="Approve" small onPress={() => decide(a.id, 'approve')} disabled={busy} style={{ flex: 1 }} />
                </View>
              </Card>
            ))}
          </>
        ) : null}

        {loading ? <View style={{ paddingVertical: space(6), alignItems: 'center' }}><ActivityIndicator /></View> : null}
        {error ? <Card><Text style={s.sub}>{error}</Text></Card> : null}

        {automations.map((a) => (
          <Card key={a.key}>
            <SwitchRow
              label={a.name}
              hint={a.purpose}
              value={a.enabled}
              onChange={(v: boolean) => toggle(a.key, v)}
              last
            />
            <Text style={s.sub}>
              {a.runs} runs · {a.acted} acted · {a.failed} failed
              {a.awaiting_approval ? ` · ${a.awaiting_approval} awaiting approval` : ''}
              {a.approval_ceiling_minor ? ` · asks above ${inr(a.approval_ceiling_minor, { noPaise: true })}` : ''}
              {(a.overridden ?? []).length ? ' · customised' : ''}
            </Text>
          </Card>
        ))}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(2) },
  name: { ...font.h3, color: color.inkStrong, marginTop: space(3) },
  sub: { ...font.small, color: color.inkSoft, marginTop: space(2), lineHeight: 18 },
});
