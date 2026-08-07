import React from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Badge, Card, CloseIcon, DottedRule, MoneyRow, color, font, inr, space, useBack } from '@dwellm8/mobile-shared';
import { useLiveData, useTicket } from '../src/data/source';

export default function Ticket() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { id } = useLocalSearchParams<{ id: string }>();
  const { leaseId } = useLiveData();
  const { loading, error, ticket: t } = useTicket(leaseId, id);

  if (loading || !t) {
    return (
      <View style={{ flex: 1, backgroundColor: color.bgTop }}>
        <SafeAreaView edges={['top']} style={{ backgroundColor: '#FFF' }}>
          <View style={s.head}>
            <Pressable onPress={goBack} hitSlop={10}><CloseIcon size={26} w={2.2} /></Pressable>
            <Text style={s.headTitle} numberOfLines={1}>Request</Text>
          </View>
        </SafeAreaView>
        <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
          {loading ? <ActivityIndicator /> : <Text style={s.cat}>{error ?? 'No such request.'}</Text>}
        </View>
      </View>
    );
  }

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <SafeAreaView edges={['top']} style={{ backgroundColor: '#FFF' }}>
        <View style={s.head}>
          <Pressable onPress={goBack} hitSlop={10}><CloseIcon size={26} w={2.2} /></Pressable>
          <Text style={s.headTitle} numberOfLines={1}>{t.title}</Text>
        </View>
      </SafeAreaView>

      <ScrollView contentContainerStyle={{ paddingVertical: space(4), paddingBottom: space(10) }}>
        <Card>
          <View style={s.top}>
            <Badge text={t.status.toUpperCase()} />
            <Text style={s.cat}>{t.category}</Text>
          </View>
          <MoneyRow label="Raised" value={t.raised} />
          {t.slot ? <MoneyRow label="Visit" value={t.slot} tone="positive" /> : null}
          {t.vendor ? <MoneyRow label="Vendor" value={t.vendor} last /> : null}
        </Card>

        <Card>
          <Text style={s.section}>Who pays</Text>
          <Text style={s.liability}>
            {t.liability === 'Tenant' ? 'You pay this'
              : t.liability === 'Owner' ? 'Your owner pays this'
              : t.liability === 'Shared' ? 'Shared cost'
              : 'Being assessed'}
          </Text>
          <Text style={s.reason}>
            {t.liabilityReason
              ?? 'Your manager confirms the split once the issue is assessed — you will see it here before any work is approved.'}
          </Text>
          {t.costPaise ? (
            <>
              <DottedRule />
              <MoneyRow
                label={t.liability === 'Tenant' ? 'Added to your next invoice' : 'Cost to the owner'}
                value={inr(t.costPaise)}
                tone={t.liability === 'Tenant' ? 'negative' : 'neutral'}
                last
              />
            </>
          ) : null}
        </Card>

        <Text style={s.heading}>Progress</Text>
        <Card>
          {t.timeline.map((e, i) => (
            <View key={i} style={s.step}>
              <View style={s.stepCol}>
                <View style={[s.dot, i === t.timeline.length - 1 && s.dotNow]} />
                {i < t.timeline.length - 1 ? <View style={s.line} /> : null}
              </View>
              <View style={{ flex: 1, paddingBottom: space(4) }}>
                <Text style={s.stepWhat}>{e.what}</Text>
                <Text style={s.stepAt}>{e.at}</Text>
              </View>
            </View>
          ))}
        </Card>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  head: { flexDirection: 'row', alignItems: 'center', gap: 14, padding: space(4) },
  headTitle: { ...font.h3, color: color.inkStrong, flex: 1 },
  top: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginBottom: space(2) },
  cat: { ...font.small, color: color.inkSoft },
  section: { ...font.label, color: color.inkSoft },
  liability: { ...font.h3, color: color.inkStrong, marginTop: 4 },
  reason: { ...font.body, color: color.ink, marginTop: 6, marginBottom: space(3), lineHeight: 22 },
  heading: { ...font.h2, color: color.inkStrong, marginHorizontal: space(4), marginTop: space(3), marginBottom: space(3) },
  step: { flexDirection: 'row', gap: 14 },
  stepCol: { width: 16, alignItems: 'center' },
  dot: { width: 11, height: 11, borderRadius: 6, backgroundColor: '#C6D4E2', marginTop: 5 },
  dotNow: { backgroundColor: color.positive },
  line: { flex: 1, width: 2, backgroundColor: '#E3EAF1', marginVertical: 3 },
  stepWhat: { ...font.body, color: color.inkStrong, lineHeight: 21 },
  stepAt: { ...font.small, color: color.inkSoft, marginTop: 3 },
});
