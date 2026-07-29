import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, Button, StatusPill, Segmented, Toast,
  Timeline, ListRow, DocIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { agreement, tenancy } from '../src/data/mock';

/**
 * Agreement, deposit and notice.
 *
 * A tenant's three anxious questions: what did I sign, where is my deposit,
 * and how do I leave. Each is answered plainly, with the dates that actually
 * bind them.
 */

export default function Tenancy() {
  const router = useRouter();
  const [tab, setTab] = useState('Agreement');
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2800);
  };

  return (
    <>
      <BackHeader title="Your tenancy" subtitle={tenancy.unit} onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <View style={{ marginTop: space(3), marginBottom: space(3) }}>
          <Segmented items={['Agreement', 'Deposit', 'Notice']} value={tab} onChange={setTab} />
        </View>

        {tab === 'Agreement' ? (
          <>
            <Card>
              <StatusPill text="Active" tone="green" dot />
              <Text style={s.h1}>{agreement.kind}</Text>
              <Text style={s.sub}>{agreement.number}</Text>
              <View style={{ marginTop: space(3) }}>
                <KeyValue k="Term" v={`${agreement.from} to ${agreement.to}`} />
                <KeyValue k="Length" v={`${agreement.months} months`} />
                <KeyValue k="Rent" v={`${inr(tenancy.rentPaise, { noPaise: true })} per month`} />
                <KeyValue k="Escalation on renewal" v={`${agreement.escalationPct}%`} />
                <KeyValue k="Lock-in ends" v={agreement.lockInEnds} />
                <KeyValue k="Notice period" v={`${agreement.noticeDays} days`} />
                <KeyValue k="Registration" v={agreement.registered} />
                <KeyValue k="Signature" v={agreement.eSigned} last />
              </View>
              <Text style={s.note}>
                Eleven months is the standard Indian term — it keeps the agreement outside rent
                control while remaining registrable. Your Aadhaar number is never stored by dwellm8.
              </Text>
            </Card>

            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              <ListRow left={<DocIcon size={20} c={color.inkFaint} />} title="Executed agreement.pdf" subtitle="Signed by both parties" onPress={() => router.push('/documents')} />
              <ListRow left={<DocIcon size={20} c={color.inkFaint} />} title="Registration receipt.pdf" subtitle={`Stamp duty ${inr(agreement.stampDutyPaise, { noPaise: true })}`} onPress={() => router.push('/documents')} last />
            </Card>
          </>
        ) : null}

        {tab === 'Deposit' ? (
          <>
            <Card>
              <Text style={s.label}>Security deposit held</Text>
              <Text style={s.big}>{inr(agreement.depositPaise)}</Text>
              <Text style={s.sub}>{agreement.depositHeldBy}</Text>
              <View style={{ marginTop: space(4) }}>
                <KeyValue k="Paid on" v="20 Apr 2026" />
                <KeyValue k="Equivalent to" v="3 months rent" />
                <KeyValue k="Deductions so far" v="None" tone="green" />
                <KeyValue k="Refund due" v="Within 30 days of move-out" last />
              </View>
              <Text style={s.note}>
                Your deposit is never set against rent without your written consent. Deductions at
                move-out must be itemised against the move-in condition report — which is in your
                documents, with photographs.
              </Text>
            </Card>
            <Card>
              <Text style={s.h}>How a refund is worked out</Text>
              <Timeline
                items={[
                  { at: 'Move-out day', what: 'Joint inspection against the move-in report' },
                  { at: 'Within 3 days', what: 'Itemised deductions shared with you, with photos' },
                  { at: 'You decide', what: 'Accept, or dispute it here' },
                  { at: 'Within 30 days', what: 'Balance refunded to your account', done: false },
                ]}
              />
            </Card>
          </>
        ) : null}

        {tab === 'Notice' ? (
          <>
            <Card>
              <Text style={s.h}>Leaving</Text>
              <Text style={s.body}>
                You must give {agreement.noticeDays} days notice. Your lock-in ends on{' '}
                {agreement.lockInEnds} — leaving before that means the lock-in rent still applies,
                and the app will show you the exact figure before you commit to anything.
              </Text>
              <View style={{ marginTop: space(3) }}>
                <KeyValue k="Earliest move-out without penalty" v="15 Oct 2026" />
                <KeyValue k="If you served notice today" v="Move out on 27 Sep 2026" />
                <KeyValue k="Lock-in charge if you leave then" v={inr(4_20_00_00)} tone="red" last />
              </View>
              <Button
                label="Start notice to vacate"
                tone="secondary"
                onPress={() => say('Draft notice created — your manager has not been told yet')}
                style={{ marginTop: space(4) }}
              />
              <Text style={s.note}>
                Starting a notice creates a draft only. Nothing is served, and your manager sees
                nothing, until you confirm it.
              </Text>
            </Card>
            <Card>
              <Text style={s.h}>Renewal</Text>
              <Text style={s.body}>
                Your manager will offer renewal terms 90 days before {agreement.to}. Rent would rise
                by {agreement.escalationPct}% to {inr(Math.round(tenancy.rentPaise * 1.05), { noPaise: true })} unless
                you negotiate otherwise.
              </Text>
            </Card>
          </>
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  h1: { ...font.h2, color: color.inkStrong, marginTop: space(3) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3 },
  label: { ...font.label, color: color.inkSoft },
  big: { fontSize: 32, fontWeight: '800', color: color.inkStrong, marginTop: 4 },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
