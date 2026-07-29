import React from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { inr, job, properties } from '../src/data/mock';
import {
  Badge,
  BathIcon,
  BedIcon,
  CarIcon,
  Card,
  CloseIcon,
  DocIcon,
  DottedRule,
  color,
  font,
  space,
} from '@rentora/mobile-shared';

export default function JobSheet() {
  const router = useRouter();
  const p = properties.find((x) => x.id === job.propertyId)!;

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <SafeAreaView edges={['top']} style={{ backgroundColor: '#FFF' }}>
        <View style={s.head}>
          <Pressable onPress={() => router.back()} hitSlop={10}><CloseIcon size={26} w={2.2} /></Pressable>
          <Text style={s.headTitle} numberOfLines={1}>{job.title}</Text>
          <Text style={s.jobNo}>#{job.id}</Text>
        </View>
      </SafeAreaView>

      <ScrollView contentContainerStyle={{ paddingVertical: space(4), paddingBottom: space(10) }}>
        <Card>
          <View style={s.addrRow}>
            <Text style={s.addr} numberOfLines={1}>{p.address}</Text>
            <View style={s.attrs}>
              <BedIcon size={19} c={color.inkSoft} /><Text style={s.attr}>{p.beds}</Text>
              <BathIcon size={19} c={color.inkSoft} /><Text style={s.attr}>{p.baths}</Text>
              <CarIcon size={19} c={color.inkSoft} /><Text style={s.attr}>{p.parking}</Text>
            </View>
          </View>
          <DottedRule />
          <View style={s.metaRow}>
            <Text style={s.metaLabel}>Completed on</Text>
            <Text style={s.metaValue}>{job.completedOn}</Text>
          </View>
        </Card>

        <Card>
          <Text style={s.label}>Description</Text>
          <Text style={s.body}>{job.description}</Text>

          <Text style={[s.label, { marginTop: space(4) }]}>More details</Text>
          <Text style={s.body}>{job.detail}</Text>

          <Text style={[s.label, { marginTop: space(4) }]}>Vendor</Text>
          <Text style={s.body}>{job.tradie}</Text>
        </Card>

        <Text style={s.section}>Quotes</Text>
        <Card>
          <View style={s.quoteHead}>
            <Badge text="ASSIGNED" />
            <Text style={s.ref}>Ref. #{job.quote.ref}</Text>
          </View>
          <Text style={s.vendor}>{job.quote.vendor}</Text>
          <DottedRule />
          <View style={s.metaRow}>
            <Text style={s.metaLabel}>Amount</Text>
            <Text style={s.amount}>{inr(job.quote.paise)}</Text>
          </View>
          <DottedRule />
          <View style={{ paddingTop: space(3) }}>
            <Text style={s.metaLabel}>Attachment</Text>
            <Pressable style={s.fileRow}>
              <DocIcon size={20} />
              <Text style={s.file}>{job.quote.file}</Text>
            </Pressable>
          </View>
        </Card>

        <Card>
          <Text style={s.label}>Who pays</Text>
          <Text style={s.body}>
            Owner-borne — asset defect on an owner-provided fixture, above the ₹1,000 tenant threshold.
            Approved within your manager's ₹5,000 spend authority, so no approval was needed from you.
          </Text>
        </Card>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  head: { flexDirection: 'row', alignItems: 'center', gap: 12, padding: space(4) },
  headTitle: { ...font.h3, color: color.inkStrong, flex: 1 },
  jobNo: { ...font.h3, color: color.inkStrong },
  addrRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 10, paddingBottom: space(3) },
  addr: { ...font.body, color: color.inkStrong, flexShrink: 1 },
  attrs: { flexDirection: 'row', alignItems: 'center', gap: 5 },
  attr: { ...font.label, color: color.inkStrong, marginRight: 6 },
  metaRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', paddingVertical: space(3) },
  metaLabel: { ...font.body, color: color.ink },
  metaValue: { ...font.body, color: color.inkStrong, fontWeight: '700' },
  label: { ...font.label, color: color.inkSoft, marginBottom: 5 },
  body: { ...font.body, color: color.inkStrong, lineHeight: 23 },
  section: { ...font.h2, color: color.inkStrong, marginHorizontal: space(4), marginTop: space(3), marginBottom: space(3) },
  quoteHead: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginBottom: space(3) },
  ref: { ...font.label, color: color.inkSoft },
  vendor: { ...font.h3, color: color.inkStrong, marginBottom: space(3) },
  amount: { ...font.h3, color: color.inkStrong },
  fileRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 6 },
  file: { ...font.body, color: color.accent, fontWeight: '600' },
});
