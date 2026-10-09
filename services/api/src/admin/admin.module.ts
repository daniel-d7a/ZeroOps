import { Module } from '@nestjs/common';
import { ProjectsModule } from '../projects/projects.module';
import { AdminController } from './admin.controller';
import { AdminSeedService } from './admin-seed.service';
import { AdminService } from './admin.service';

@Module({
  imports: [ProjectsModule], // ActionsService (admin stop)
  controllers: [AdminController],
  providers: [AdminService, AdminSeedService],
})
export class AdminModule {}
