import React, { useState } from 'react';
import { Text, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, Segmented, Timeline,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { useLiveData } from '../src/data/source';

/**
 * Agreement, deposit and notice.
 *
 * A tenant's three anxious questions: what did I sign, where is my deposit,
 * and how do I leave. Each is answered from the lease record — and where the
 * record has no answer yet, the screen says so instead of inventing one.
 */

export default function Tenancy() {
  const router = useRouter();
  const [tab, setTab] = useState('Agreement');
  const { tenancy, loading } = useLiveData();
  const live = tenancy.state === 'active' || tenancy.state === 'notice';

  return (
    <>
      <BackHeader title="Your tenancy" subtitle={tenancy.unit} onBack={() => router.back()} />
      <Screen>
        <View style={{ marginTop: space(3), marginBottom: space(3) }}>
          <Segmented items={['Agreement', 'Deposit', 'Notice']} value={tab} onChange={setTab} />
        </View>

        {tab === 'Agreement' ? (
          <Card>
            <StatusPill
              text={loading ? '…' : tenancy.state ? tenancy.state.replace(/^\w/, (c) => c.toUpperCase()) : 'Unknown'}
              tone={live ? 'green' : 'neutral'}
              dot
            />
            <Text style={s.h1}>Tenancy at {tenancy.unit}</Text>
            <Text style={s.sub}>{tenancy.locality}</Text>
            <View style={{ marginTop: space(3) }}>
              <KeyValue k="Term" v={tenancy.endOn ? `${tenancy.startOn} to ${tenancy.endOn}` : `From ${tenancy.startOn}, open-ended`} />
              <KeyValue k="Rent" v={`${inr(tenancy.rentPaise, { noPaise: true })} per month`} />
              <KeyValue k="Due day" v={`${tenancy.dueDay} of the month`} />
              {tenancy.lockInUntil ? <KeyValue k="Lock-in ends" v={tenancy.lockInUntil} /> : null}
              <KeyValue k="Notice period" v={`${tenancy.noticeDays} days`} />
              <KeyValue k="Managed by" v={tenancy.agency} last />
            </View>
            <Text style={s.note}>
              These are the terms as your landlord recorded them. If anything here does not match
              what you signed, raise it with your manager — the record, not memory, settles it.
            </Text>
          </Card>
        ) : null}

        {tab === 'Deposit' ? (
          <>
            <Card>
              <Text style={s.label}>Security deposit held</Text>
              <Text style={s.big}>{inr(tenancy.depositPaise)}</Text>
              <Text style={s.sub}>Held against the tenancy, as the ledger records it</Text>
              <Text style={s.note}>
                Your deposit is never set against rent without your written consent. Deductions at
                move-out must be itemised against the move-in condition report.
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
          <Card>
            <Text style={s.h}>Leaving</Text>
            <Text style={s.body}>
              You must give {tenancy.noticeDays} days notice.
              {tenancy.lockInUntil
                ? ` Your lock-in ends on ${tenancy.lockInUntil} — leaving before that means the lock-in rent still applies.`
                : ''}
            </Text>
            <View style={{ marginTop: space(3) }}>
              {tenancy.lockInUntil ? <KeyValue k="Earliest move-out without penalty" v={tenancy.lockInUntil} /> : null}
              {tenancy.endOn ? <KeyValue k="Lease ends" v={tenancy.endOn} last /> : <KeyValue k="Term" v="Open-ended" last />}
            </View>
            <Text style={s.note}>
              Serving notice from the app is coming soon. Until then, talk to your manager — and
              anything you agree lands on this record.
            </Text>
          </Card>
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
