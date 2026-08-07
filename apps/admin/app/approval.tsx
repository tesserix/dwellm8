import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, StatusPill, Button, ActionBar, KeyValue, Field,
  Toast, Timeline,
  color, font, inr, space, useBack,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { approvals } from '../src/data/mock';

/** One approval — the reasoning, then a decision that must carry a reason. */

const riskTone: Record<string, Tone> = { Low: 'green', Medium: 'amber', High: 'red' };

export default function ApprovalScreen() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { id } = useLocalSearchParams<{ id?: string }>();
  const a = approvals.find((x) => x.id === id) ?? approvals[0];

  const [reason, setReason] = useState('');
  const [decided, setDecided] = useState<string | null>(null);

  if (decided) {
    return (
      <>
        <BackHeader title="Decision recorded" onBack={goBack} />
        <Screen>
          <Toast text={`${decided} — written to the audit trail`} />
          <Card>
            <StatusPill text={decided} tone={decided === 'Approved' ? 'green' : 'red'} />
            <Text style={s.title}>{a.subject}</Text>
            <KeyValue k="Type" v={a.kind} />
            <KeyValue k="Decided by" v="Kavya Desai · Platform Operations" />
            <KeyValue k="Reason" v={reason || 'Not given'} last />
          </Card>
          <Button label="Back to the queue" onPress={goBack} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title={a.kind} subtitle={a.subject} onBack={goBack} />
      <Screen>
        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <StatusPill text={`${a.risk} risk`} tone={riskTone[a.risk]} />
            <View style={{ flex: 1 }} />
            {a.amountPaise ? <Text style={s.amt}>{inr(a.amountPaise)}</Text> : null}
          </View>
          <Text style={s.title}>{a.subject}</Text>
          <Text style={s.body}>{a.why}</Text>
          <View style={{ marginTop: space(3) }}>
            <KeyValue k="Requested by" v={a.requestedBy} />
            <KeyValue k="Waiting since" v={a.at} last />
          </View>
        </Card>

        <Card>
          <Text style={s.h}>Checks</Text>
          <Timeline
            items={[
              { at: 'Automatic', what: 'PAN and GSTIN verified against the registry' },
              { at: 'Automatic', what: 'Bank penny-drop matched the account name' },
              { at: 'Automatic', what: 'Sanctions and adverse media screen — clear' },
              { at: 'Needs you', what: a.risk === 'High' ? 'Statutory retention conflict must be judged by a human' : 'Director phone number matches a suspended organisation', done: false },
            ]}
          />
        </Card>

        <Card>
          <Field
            label="Reason for the decision (recorded)"
            value={reason}
            onChange={setReason}
            placeholder="Confirmed with the director; the shared number is a shared office line."
            multiline
          />
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Decline" tone="secondary" onPress={() => setDecided('Declined')} disabled={!reason} style={{ flex: 1 }} />
        <Button label="Approve" onPress={() => setDecided('Approved')} disabled={!reason} style={{ flex: 1.4 }} />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  title: { ...font.h2, color: color.inkStrong, marginTop: space(3) },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  amt: { ...font.h3, color: color.inkStrong },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(2) },
});
